package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"uranus/internal/config"
)

// fakeDir holds fake nginx and systemctl executables that record their calls
// in calls.log; nginx -V points the conf and pid files into the same directory.
//
// The fake nginx behaves like the real one where it matters: `-t` and `-T`
// fail when FAKE_NGINX_TEST_FAIL is set (`-T` dumps the .conf files of the
// directory and of conf.d), `-s quit` lets the master exit a second later,
// and starting while the old master still holds the port fails. When started
// with FAKE_NGINX_LOG_TO_STDERR set, the master keeps stdout and stderr open
// as nginx logging to stderr does; with FAKE_NGINX_NO_PID it never writes its
// pid file, as a master that exits right after starting.
//
// The fake `systemctl show` reports the units listed in FAKE_SYSTEMD_UNITS as
// installed, and those in FAKE_SYSTEMD_ACTIVE as running.
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
		"if [ \"$1\" = \"-c\" ]; then shift 2; fi\n" +
		"PID=\"$DIR/nginx.pid\"\n" +
		"if [ \"$1\" = \"-t\" ]; then\n" +
		"  if [ -n \"$FAKE_NGINX_TEST_FAIL\" ]; then echo \"$FAKE_NGINX_TEST_FAIL\" >&2; exit 1; fi\n" +
		"  echo 'nginx: configuration file test is successful' >&2; exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"-T\" ]; then\n" +
		"  if [ -n \"$FAKE_NGINX_TEST_FAIL\" ]; then echo \"$FAKE_NGINX_TEST_FAIL\" >&2; exit 1; fi\n" +
		"  for f in \"$DIR\"/*.conf \"$DIR\"/conf.d/*; do\n" +
		"    if [ -f \"$f\" ]; then printf '# configuration file %s:\\n' \"$f\"; cat \"$f\"; echo; fi\n" +
		"  done\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ -n \"$FAKE_NGINX_FAIL\" ]; then echo \"$FAKE_NGINX_FAIL\" >&2; exit 1; fi\n" +
		"case \"$*\" in\n" +
		"  '-s quit'|'-s stop') ( sleep 1; rm -f \"$PID\" ) >/dev/null 2>&1 & ;;\n" +
		"  '') if [ -f \"$PID\" ] && kill -0 \"$(cat \"$PID\")\" 2>/dev/null; then\n" +
		"        echo 'nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)' >&2; exit 1\n" +
		"      fi\n" +
		"      if [ -n \"$FAKE_NGINX_NO_PID\" ]; then exit 0; fi\n" +
		"      echo $PPID > \"$PID\"\n" +
		"      if [ -n \"$FAKE_NGINX_LOG_TO_STDERR\" ]; then sleep 20 & echo $! > \"$DIR/master.pid\"; fi ;;\n" +
		"esac\n"
	systemctl := "#!/bin/sh\n" +
		"DIR='" + fakeDir + "'\n" +
		"if [ \"$1\" = show ]; then\n" +
		"  for unit; do :; done\n" +
		"  case \" $FAKE_SYSTEMD_UNITS \" in\n" +
		"  *\" $unit \"*)\n" +
		"    echo LoadState=loaded\n" +
		"    case \" $FAKE_SYSTEMD_ACTIVE \" in *\" $unit \"*) echo ActiveState=active ;; *) echo ActiveState=inactive ;; esac\n" +
		"    echo \"ExecStart={ path=$DIR/nginx ; argv[]=$DIR/nginx -g daemon on; master_process on; ; ignore_errors=no ; pid=0 ; code=(null) ; status=0/0 }\" ;;\n" +
		"  *) echo LoadState=not-found; echo ActiveState=inactive ;;\n" +
		"  esac\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo systemctl \"$@\" >> \"$DIR/calls.log\"\n"
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

// takeRawCalls returns the commands run since the last call.
func takeRawCalls(t *testing.T) []string {
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

// takeCalls returns the commands run since the last call, without the -c
// argument every nginx command gets (TestNginxCommandsUseTheMainConfig checks it).
func takeCalls(t *testing.T) []string {
	t.Helper()
	conf := " -c " + filepath.Join(fakeDir, "custom-main.conf")
	calls := takeRawCalls(t)
	for i, c := range calls {
		calls[i] = strings.Replace(c, conf, "", 1)
	}
	return calls
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
	t.Setenv("FAKE_SYSTEMD_UNITS", "nginx.service")
	writePidFile(t, 0)
	if got := StartNginx(); got != "OK" {
		t.Fatalf("start: %q", got)
	}
	// systemd runs nginx from now on
	t.Setenv("FAKE_SYSTEMD_ACTIVE", "nginx.service")
	writePidFile(t, os.Getpid())
	if got := ReloadNginx(); got != "OK" {
		t.Fatalf("reload: %q", got)
	}
	if got := StopNginx(); got != "OK" {
		t.Fatalf("stop: %q", got)
	}
	assertCalls(t, "nginx -t", "systemctl start nginx.service", "nginx -t",
		"systemctl reload nginx.service", "systemctl stop nginx.service")
}

// nginx built from source on a systemd host usually has no unit: systemctl
// start nginx used to fail with "Unit nginx.service not found".
func TestNginxControlOnSystemdHostWithoutUnit(t *testing.T) {
	simulateHost(t, "linux", true)
	runAllActions(t)
	assertCalls(t, "nginx -t", "nginx", "nginx -t", "nginx -s reload", "nginx -s quit")
	if got := NginxManager(); got != "直接控制 nginx 进程（nginx -s）" {
		t.Fatalf("NginxManager() = %q", got)
	}
}

// nginx started by hand while nginx.service exists: systemctl reload refused
// ("nginx.service is not active"), systemctl stop did nothing and left nginx
// running, and systemctl restart started a second master on a busy port.
func TestNginxStartedByHandIsSignalledDirectly(t *testing.T) {
	simulateHost(t, "linux", true)
	t.Setenv("FAKE_SYSTEMD_UNITS", "nginx.service")
	writePidFile(t, os.Getpid())

	if got := ReloadNginx(); got != "OK" {
		t.Fatalf("reload: %q", got)
	}
	assertCalls(t, "nginx -t", "nginx -s reload")

	// Afterwards systemd manages it.
	if got := RestartNginx(); got != "OK" {
		t.Fatalf("restart: %q", got)
	}
	assertCalls(t, "nginx -t", "nginx -s quit", "systemctl start nginx.service")
}

// OpenResty's packages install openresty.service.
func TestOpenRestyUnit(t *testing.T) {
	simulateHost(t, "linux", true)
	t.Setenv("FAKE_SYSTEMD_UNITS", "openresty.service")
	t.Setenv("FAKE_SYSTEMD_ACTIVE", "openresty.service")
	writePidFile(t, os.Getpid())

	if got := ReloadNginx(); got != "OK" {
		t.Fatalf("reload: %q", got)
	}
	assertCalls(t, "nginx -t", "systemctl reload openresty.service")
	if got := NginxManager(); got != "systemd（openresty.service）" {
		t.Fatalf("NginxManager() = %q", got)
	}
}

// Every nginx command names the main configuration file: the one shown in the
// editor, and the one a systemd unit may start nginx with instead of the
// compiled-in path.
func TestNginxCommandsUseTheMainConfig(t *testing.T) {
	simulateHost(t, "linux", false)
	runAllActions(t)
	conf := filepath.Join(fakeDir, "custom-main.conf")
	calls := takeRawCalls(t)
	if len(calls) != 5 {
		t.Fatalf("calls: %q", calls)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "nginx -c "+conf) {
			t.Errorf("%q does not name %s", c, conf)
		}
	}
}

// The pid directive of nginx.conf overrides the compiled-in pid file.
func TestNginxStatusFollowsThePidDirective(t *testing.T) {
	conf := filepath.Join(fakeDir, "custom-main.conf")
	pidFile := filepath.Join(fakeDir, "elsewhere.pid")
	if err := os.WriteFile(conf, []byte("pid "+pidFile+";\nevents {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(conf); os.Remove(pidFile) })
	writePidFile(t, 0)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	resetStatusCache()
	if got := NginxStatus(); got != strconv.Itoa(os.Getpid()) {
		t.Fatalf("NginxStatus() = %q", got)
	}
}

// Without an include of the sites directory nginx never loads the sites:
// source builds, OpenResty and Arch Linux ship such a nginx.conf.
func TestIncludeSitesDir(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, 0)
	conf := filepath.Join(fakeDir, "custom-main.conf")
	sitesDir := filepath.Join(fakeDir, "conf.d")
	original := "events {}\nhttp {\n    server { listen 80; }\n}\n"
	if err := os.WriteFile(conf, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(conf); os.RemoveAll(sitesDir) })
	if sites := config.Sites(); sites.Included || sites.Dir != sitesDir {
		t.Fatalf("before: %+v", sites)
	}

	t.Setenv("FAKE_NGINX_TEST_FAIL", "nginx: [emerg] something else is broken")
	if got := IncludeSitesDir(); !strings.Contains(got, "something else is broken") {
		t.Fatalf("failed check not reported: %q", got)
	}
	if content, _ := os.ReadFile(conf); string(content) != original {
		t.Fatalf("nginx.conf not restored:\n%s", content)
	}

	t.Setenv("FAKE_NGINX_TEST_FAIL", "")
	if got := IncludeSitesDir(); got != "OK" {
		t.Fatalf("IncludeSitesDir() = %q", got)
	}
	if sites := config.Sites(); !sites.Included || sites.Dir != sitesDir {
		t.Fatalf("after: %+v", sites)
	}
	if info, err := os.Stat(sitesDir); err != nil || !info.IsDir() {
		t.Fatalf("sites directory not created: %v", err)
	}
	content, _ := os.ReadFile(conf)
	if got := IncludeSitesDir(); got != "OK" {
		t.Fatalf("second call: %q", got)
	}
	if again, _ := os.ReadFile(conf); string(again) != string(content) {
		t.Fatalf("include added twice:\n%s", again)
	}
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

// nginx logging to stderr keeps the output of the start command open while it
// runs: the official Docker image links its logs to /dev/stdout and
// /dev/stderr, others use `error_log stderr`. Starting it never returned.
func TestStartNginxLoggingToStderr(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, 0)
	t.Setenv("FAKE_NGINX_LOG_TO_STDERR", "1")
	t.Cleanup(func() {
		master := filepath.Join(fakeDir, "master.pid")
		data, _ := os.ReadFile(master)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		os.Remove(master)
	})

	begin := time.Now()
	if got := StartNginx(); got != "OK" {
		t.Fatalf("start: %q", got)
	}
	if elapsed := time.Since(begin); elapsed > 5*time.Second {
		t.Fatalf("start returned after %v, when nginx closed its output", elapsed.Round(time.Second))
	}
	assertCalls(t, "nginx -t", "nginx")
}

// The start command succeeds as soon as nginx forked into the background; the
// master can still exit right away (it cannot write its pid file, say).
func TestStartNginxReportsNginxThatIsNotRunning(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, 0)
	t.Setenv("FAKE_NGINX_NO_PID", "1")
	saved := nginxStartTimeout
	nginxStartTimeout = 300 * time.Millisecond
	t.Cleanup(func() { nginxStartTimeout = saved })

	got := StartNginx()
	if got == "OK" || !strings.Contains(got, config.NginxPidFile()) {
		t.Fatalf("start of an nginx that does not run: %q", got)
	}
	writePidFile(t, 0)
	if got := RestartNginx(); got == "OK" {
		t.Fatalf("restart of an nginx that does not run: %q", got)
	}
}

func TestRestartNginxWithSystemd(t *testing.T) {
	simulateHost(t, "linux", true)
	t.Setenv("FAKE_SYSTEMD_UNITS", "nginx.service")
	t.Setenv("FAKE_SYSTEMD_ACTIVE", "nginx.service")
	writePidFile(t, os.Getpid())
	if got := RestartNginx(); got != "OK" {
		t.Fatalf("restart: %q", got)
	}
	assertCalls(t, "nginx -t", "systemctl restart nginx.service")
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
