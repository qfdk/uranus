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
		"if [ -n \"$FAKE_NGINX_FAIL\" ]; then echo \"$FAKE_NGINX_FAIL\" >&2; exit 1; fi\n"
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

func runAllActions(t *testing.T) {
	t.Helper()
	writePidFile(t, os.Getpid())
	for name, action := range map[string]func() string{"start": StartNginx, "reload": ReloadNginx, "stop": StopNginx} {
		if got := action(); got != "OK" {
			t.Fatalf("%s: %q", name, got)
		}
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
	assertCalls(t, "nginx", "nginx -s reload", "nginx -s stop")
}

func TestNginxControlOnLinuxWithSystemd(t *testing.T) {
	simulateHost(t, "linux", true)
	runAllActions(t)
	assertCalls(t, "systemctl start nginx", "systemctl reload nginx", "systemctl stop nginx")
}

func TestNginxControlOnMacOS(t *testing.T) {
	simulateHost(t, "darwin", true)
	runAllActions(t)
	assertCalls(t, "nginx", "nginx -s reload", "nginx -s stop")
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
