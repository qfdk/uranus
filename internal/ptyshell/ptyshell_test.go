package ptyshell

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// waitFor polls the shell output until re matches or the timeout expires.
func (o *output) waitFor(re *regexp.Regexp, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m := re.FindStringSubmatch(o.String()); m != nil {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// startShell starts shell (the default one when empty) without user rc files
// and collects its output.
func startShell(t *testing.T, shell string) (*Shell, *output) {
	t.Helper()
	if shell == "" {
		shell = DefaultShell()
	}
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm",
		"PS1=$ ",
		"BASH_SILENCE_DEPRECATION_WARNING=1",
	}
	s, err := start(shell, env, home, 24, 80)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return s, collect(t, s)
}

// collect closes s when the test ends and gathers its output meanwhile.
func collect(t *testing.T, s *Shell) *output {
	t.Helper()
	t.Cleanup(func() {
		s.Close()
		// Let the shell exit (bash writes its history into HOME) before HOME is removed.
		select {
		case <-s.Exited():
		case <-time.After(5 * time.Second):
			t.Errorf("shell %d did not exit", s.Pid())
		}
	})

	out := &output{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := s.PTY().Read(b)
			if n > 0 {
				out.mu.Lock()
				out.buf.Write(b[:n])
				out.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		m[name] = value
	}
	return m
}

// The terminal must not inherit the service's own variables, and gets a UTF-8
// locale when the service has none.
func TestLoginEnv(t *testing.T) {
	t.Setenv("HOME", "/home/test")
	t.Setenv("INVOCATION_ID", "0123abcd")
	t.Setenv("GIN_MODE", "release")
	t.Setenv("LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LC_TIME", "en_GB.UTF-8")

	env, home := loginEnv("/bin/bash")
	got := envMap(env)
	if home != "/home/test" || got["HOME"] != "/home/test" {
		t.Errorf("home = %q, HOME = %q, want /home/test", home, got["HOME"])
	}
	for name, want := range map[string]string{
		"SHELL": "/bin/bash", "TERM": "xterm-256color", "COLORTERM": "truecolor",
		"LANG": "C.UTF-8", "LC_TIME": "en_GB.UTF-8", "PATH": os.Getenv("PATH"),
	} {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
	for _, name := range []string{"INVOCATION_ID", "GIN_MODE", "LC_ALL"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s leaked into the terminal environment", name)
		}
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	if env, _ := loginEnv("/bin/bash"); envMap(env)["LANG"] != "zh_CN.UTF-8" {
		t.Errorf("the service's LANG was replaced: %q", envMap(env)["LANG"])
	}
}

// Like an SSH login: a login shell, started in the home directory.
func TestStartLoginShellInHome(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("INVOCATION_ID", "0123abcd")

	s, err := Start("/bin/bash", 24, 80)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out := collect(t, s)
	send(t, s, `shopt -q login_shell && L=yes; echo "R:$L:$(pwd -P):$SHELL:${INVOCATION_ID-none}:E"`+"\r")
	m := out.waitFor(regexp.MustCompile(`R:(\w*):(.*):(.*):(.*):E`), 5*time.Second)
	if m == nil {
		t.Fatalf("no answer, output:\n%s", out)
	}
	want, _ := filepath.EvalSymlinks(home)
	if m[1] != "yes" || m[2] != want || m[3] != "/bin/bash" || m[4] != "none" {
		t.Fatalf("login=%q dir=%q SHELL=%q INVOCATION_ID=%q, want yes %q /bin/bash none", m[1], m[2], m[3], m[4], want)
	}
}

func send(t *testing.T, s *Shell, input string) {
	t.Helper()
	if _, err := s.PTY().Write([]byte(input)); err != nil {
		t.Fatalf("write %q: %v", input, err)
	}
}

func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func waitGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if processGone(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Opening /dev/tty only works for a process that has a controlling terminal.
func TestShellHasControllingTerminal(t *testing.T) {
	s, out := startShell(t, "")
	send(t, s, "exec 3</dev/tty && echo CTTY_$((40+2))\r")
	if out.waitFor(regexp.MustCompile(`CTTY_42`), 5*time.Second) == nil {
		t.Fatalf("shell has no controlling terminal, output:\n%s", out)
	}
}

// Ctrl+C written to the terminal must interrupt the foreground job through the
// tty line discipline, without any signal sent by uranus itself.
func TestCtrlCInterruptsForegroundJob(t *testing.T) {
	s, out := startShell(t, "")
	send(t, s, "echo START_$((1+2)); sleep 30\r")
	if out.waitFor(regexp.MustCompile(`START_3`), 5*time.Second) == nil {
		t.Fatalf("command did not start, output:\n%s", out)
	}
	time.Sleep(300 * time.Millisecond) // let sleep become the foreground job

	send(t, s, "\x03")
	// Typed ahead: runs only once the 30s sleep is gone.
	send(t, s, "echo DONE_$((2+3))\r")
	if out.waitFor(regexp.MustCompile(`DONE_5`), 5*time.Second) == nil {
		t.Fatalf("Ctrl+C did not interrupt the foreground job, output:\n%s", out)
	}
	// The terminal echoes ^C itself, so web clients must not echo it locally.
	if got := out.String(); !bytes.Contains([]byte(got), []byte("^C")) {
		t.Fatalf("terminal did not echo ^C, output:\n%s", got)
	}
}

// Closing the terminal hangs up the session: bash passes the hangup on to its
// background jobs, so they must not outlive the terminal. (Shells such as
// busybox ash leave running background jobs alone, as in any terminal.)
func TestCloseHangsUpBackgroundJobs(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	s, out := startShell(t, "/bin/bash")
	send(t, s, "sleep 1000 & echo BG_PID=$!\r")
	m := out.waitFor(regexp.MustCompile(`BG_PID=(\d+)`), 5*time.Second)
	if m == nil {
		t.Fatalf("background job did not start, output:\n%s", out)
	}
	bgPid, _ := strconv.Atoi(m[1])

	s.Close()
	select {
	case <-s.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("shell still running 5s after Close")
	}
	if !waitGone(bgPid, 5*time.Second) {
		syscall.Kill(bgPid, syscall.SIGKILL)
		t.Fatalf("background job %d survived the hangup", bgPid)
	}
}

// A shell that exits on its own is reaped instead of staying a zombie.
func TestExitedShellIsReaped(t *testing.T) {
	s, _ := startShell(t, "")
	send(t, s, "exit\r")
	select {
	case <-s.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not exit")
	}
	if !processGone(s.Pid()) {
		t.Fatalf("shell %d still in the process table after exit", s.Pid())
	}
}
