package ptyshell

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"strconv"
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
	env := []string{
		"HOME=" + t.TempDir(),
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm",
		"PS1=$ ",
		"BASH_SILENCE_DEPRECATION_WARNING=1",
	}
	s, err := Start(shell, env, 24, 80)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
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
	return s, out
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
