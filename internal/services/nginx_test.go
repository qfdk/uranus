package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeDir holds fake nginx and systemctl executables that record their calls
// in calls.log; nginx -V points the conf and pid files into the same directory.
//
// The fake nginx behaves like the real one where it matters: `-t` fails when
// FAKE_NGINX_TEST_FAIL is set, `-s quit` lets the master exit a second later,
// and starting while the old master still holds the port fails.
var fakeDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uranus-services-test")
	if err != nil {
		panic(err)
	}
	fakeDir, _ = filepath.EvalSymlinks(dir)

	nginx := "#!/bin/sh\n" +
		"DIR='" + fakeDir + "'\n" +
		"if [ \"$1\" = \"-V\" ]; then\n" +
		"  echo 'nginx version: nginx/1.27.0' >&2\n" +
		"  echo 'built by gcc 12' >&2\n" +
		"  echo \"configure arguments: --prefix=$DIR --conf-path=$DIR/custom-main.conf --pid-path=$DIR/nginx.pid\" >&2\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo nginx \"$@\" >> \"$DIR/calls.log\"\n" +
		"PID=\"$DIR/nginx.pid\"\n" +
		"if [ \"$1\" = \"-t\" ]; then\n" +
		"  if [ -n \"$FAKE_NGINX_TEST_FAIL\" ]; then echo \"$FAKE_NGINX_TEST_FAIL\" >&2; exit 1; fi\n" +
		"  echo 'nginx: configuration file test is successful' >&2; exit 0\n" +
		"fi\n" +
		"if [ -n \"$FAKE_NGINX_FAIL\" ]; then echo \"$FAKE_NGINX_FAIL\" >&2; exit 1; fi\n" +
		"case \"$*\" in\n" +
		"  '-s quit'|'-s stop') ( sleep 1; rm -f \"$PID\" ) >/dev/null 2>&1 & ;;\n" +
		"  '') if [ -f \"$PID\" ] && kill -0 \"$(cat \"$PID\")\" 2>/dev/null; then\n" +
		"        echo 'nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)' >&2; exit 1\n" +
		"      fi\n" +
		"      echo $PPID > \"$PID\" ;;\n" +
		"esac\n"
	systemctl := "#!/bin/sh\necho systemctl \"$@\" >> '" + fakeDir + "/calls.log'\n"
	for name, script := range map[string]string{"nginx": nginx, "systemctl": systemctl} {
		if err := os.WriteFile(filepath.Join(fakeDir, name), []byte(script), 0o755); err != nil {
			panic(err)
		}
	}
	os.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// App config for code paths that read it; no control center, so nothing is sent anywhere.
	cfg := "uuid = \"test-uuid\"\nip = \"127.0.0.1\"\ntoken = \"test-token\"\n" +
		"sessionsecret = \"test-secret\"\ncontrolcenter = \"\"\n"
	if err := os.WriteFile(filepath.Join(fakeDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		panic(err)
	}
	if err := os.Chdir(fakeDir); err != nil {
		panic(err)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// takeCalls returns the commands run since the last call.
func takeCalls(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(fakeDir, "calls.log")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// simulateHost pretends to run on goos, booted with systemd or not.
func simulateHost(t *testing.T, goos string, systemd bool) {
	t.Helper()
	savedOS, savedDir := sysType, systemdRunDir
	t.Cleanup(func() { sysType, systemdRunDir = savedOS, savedDir })

	sysType = goos
	systemdRunDir = filepath.Join(t.TempDir(), "run-systemd-system")
	if systemd {
		if err := os.Mkdir(systemdRunDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	takeCalls(t)
}

func resetStatusCache() {
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Time{}
	nginxStatusMutex.Unlock()
}

// writePidFile makes nginx look like it runs as pid (0 removes the pid file).
func writePidFile(t *testing.T, pid int) {
	t.Helper()
	path := filepath.Join(fakeDir, "nginx.pid")
	if pid == 0 {
		os.Remove(path)
	} else if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resetStatusCache()
}

// runAllActions starts a stopped nginx, then reloads and stops a running one.
func runAllActions(t *testing.T) {
	t.Helper()
	writePidFile(t, 0)
	if got := StartNginx(); got != "OK" {
		t.Fatalf("start: %q", got)
	}
	writePidFile(t, os.Getpid()) // the fake systemctl starts nothing
	if got := ReloadNginx(); got != "OK" {
		t.Fatalf("reload: %q", got)
	}
	if got := StopNginx(); got != "OK" {
		t.Fatalf("stop: %q", got)
	}
}

func assertCalls(t *testing.T, want ...string) {
	t.Helper()
	got := takeCalls(t)
	if len(got) != len(want) {
		t.Fatalf("commands run: %q, want %q", got, want)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c] = true
	}
	for _, c := range want {
		if !seen[c] {
			t.Fatalf("commands run: %q, want %q", got, want)
		}
	}
}

// Docker containers (like the provided Alpine image) and OpenRC hosts have no
// systemd: nginx has to be signalled directly.
func TestNginxControlOnLinuxWithoutSystemd(t *testing.T) {
	simulateHost(t, "linux", false)
	runAllActions(t)
	// stop is the graceful quit, and the configuration is checked before start and reload
	assertCalls(t, "nginx -t", "nginx", "nginx -t", "nginx -s reload", "nginx -s quit")
}

func TestNginxControlOnLinuxWithSystemd(t *testing.T) {
	simulateHost(t, "linux", true)
	runAllActions(t)
	assertCalls(t, "nginx -t", "systemctl start nginx", "nginx -t", "systemctl reload nginx", "systemctl stop nginx")
}

func TestNginxControlOnMacOS(t *testing.T) {
	simulateHost(t, "darwin", true)
	runAllActions(t)
	assertCalls(t, "nginx -t", "nginx", "nginx -t", "nginx -s reload", "nginx -s quit")
}

func TestNginxFailureReportsNginxOutput(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	t.Setenv("FAKE_NGINX_FAIL", `nginx: [emerg] unknown directive "foo"`)
	if got := ReloadNginx(); !strings.Contains(got, `unknown directive "foo"`) {
		t.Fatalf("reload failure not reported, got %q", got)
	}
}

// A crashed nginx leaves its pid file behind; it must not be shown as running.
func TestNginxStatusIgnoresStalePidFile(t *testing.T) {
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	writePidFile(t, exited.Process.Pid)
	if got := NginxStatus(); got != "KO" {
		t.Fatalf("stale pid file reported as running: %q", got)
	}

	writePidFile(t, os.Getpid())
	if got := NginxStatus(); got != strconv.Itoa(os.Getpid()) {
		t.Fatalf("running nginx reported as %q", got)
	}

	writePidFile(t, 0)
	if got := NginxStatus(); got != "KO" {
		t.Fatalf("missing pid file reported as %q", got)
	}
}

// The editor shows the file named by --conf-path, so saving must write that
// same file, whatever its name.
func TestSaveNginxConfWritesTheMainConfigFile(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, 0)

	if got := SaveNginxConf("events {}\n"); got != "OK" {
		t.Fatalf("SaveNginxConf: %q", got)
	}
	content, err := os.ReadFile(filepath.Join(fakeDir, "custom-main.conf"))
	if err != nil || string(content) != "events {}\n" {
		t.Fatalf("main config not written: %q %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(fakeDir, "nginx.conf")); err == nil {
		t.Fatal("wrote a separate nginx.conf next to the real config file")
	}
}

// A broken configuration must stop start, reload and restart before they touch
// nginx, and the reason has to reach the user.
func TestNginxActionsRefuseBrokenConfig(t *testing.T) {
	simulateHost(t, "linux", false)
	t.Setenv("FAKE_NGINX_TEST_FAIL", `nginx: [emerg] unknown directive "foo" in /etc/nginx/conf.d/a.conf:3`)

	writePidFile(t, 0)
	if got := StartNginx(); !strings.Contains(got, `unknown directive "foo"`) {
		t.Fatalf("start with a broken config: %q", got)
	}
	writePidFile(t, os.Getpid())
	for name, action := range map[string]func() string{"reload": ReloadNginx, "restart": RestartNginx} {
		if got := action(); !strings.Contains(got, "配置检查未通过") || !strings.Contains(got, "a.conf:3") {
			t.Fatalf("%s with a broken config: %q", name, got)
		}
	}
	assertCalls(t, "nginx -t", "nginx -t", "nginx -t")
}

// Signalling nginx returns before the old master has exited; starting right
// away fails because it still holds the port.
func TestRestartNginxWaitsForTheOldMaster(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())

	if got := RestartNginx(); got != "OK" {
		t.Fatalf("restart: %q", got)
	}
	assertCalls(t, "nginx -t", "nginx -s quit", "nginx")
	if NginxStatus() == "KO" {
		t.Fatal("nginx is not running after the restart")
	}
}

func TestRestartNginxWithSystemd(t *testing.T) {
	simulateHost(t, "linux", true)
	writePidFile(t, os.Getpid())
	if got := RestartNginx(); got != "OK" {
		t.Fatalf("restart: %q", got)
	}
	assertCalls(t, "nginx -t", "systemctl restart nginx")
}

// A configuration that nginx rejects must not stay on disk, where the next
// restart or reboot would take every site down.
func TestWriteConfigCheckedRestoresOnFailure(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.conf")
	if err := os.WriteFile(existing, []byte("server { listen 80; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(dir, "added.conf")

	t.Setenv("FAKE_NGINX_TEST_FAIL", "nginx: [emerg] unexpected end of file")
	for _, path := range []string{existing, added} {
		if got := WriteConfigChecked(path, []byte("server {")); !strings.Contains(got, "unexpected end of file") {
			t.Fatalf("%s: %q", path, got)
		}
	}
	if content, _ := os.ReadFile(existing); string(content) != "server { listen 80; }\n" {
		t.Fatalf("existing file not restored: %q", content)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatal("rejected new file left on disk")
	}
	assertCalls(t, "nginx -t", "nginx -t") // nothing was reloaded

	t.Setenv("FAKE_NGINX_TEST_FAIL", "")
	if got := WriteConfigChecked(added, []byte("server { listen 8080; }\n")); got != "OK" {
		t.Fatalf("valid config: %q", got)
	}
	assertCalls(t, "nginx -t", "nginx -t", "nginx -s reload")
}
