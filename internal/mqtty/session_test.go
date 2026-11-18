package mqtty

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const testToken = "test-token-5f2a9c41d7e8"

// TestMain points the app config at a throwaway config.toml so tests never
// touch the real one or the network.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uranus-mqtty-test")
	if err != nil {
		panic(err)
	}
	cfg := "uuid = \"test-uuid\"\nip = \"127.0.0.1\"\ntoken = \"" + testToken + "\"\n" +
		"sessionsecret = \"test-session-secret\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type fakeMessage struct {
	topic   string
	payload string
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 1 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return []byte(m.payload) }
func (m fakeMessage) Ack()              {}

// The legacy uranus/terminal/* topics can open a root shell, so they need the
// agent token just like uranus/command/<uuid>.
func TestLegacyTerminalTopicsRequireToken(t *testing.T) {
	isolateHome(t)
	m := NewSessionManager()
	t.Cleanup(func() {
		for _, id := range m.ListSessions() {
			if s, err := m.GetSession(id); err == nil {
				closeAndWait(t, s)
			}
		}
		m.CloseAll()
	})

	handleMessage(nil, fakeMessage{
		topic:   "uranus/terminal/control",
		payload: `{"sessionId":"no-token","type":"create"}`,
	}, "uranus/terminal", m)
	if _, err := m.GetSession("no-token"); err == nil {
		t.Fatal("terminal session created without a token")
	}

	handleMessage(nil, fakeMessage{
		topic:   "uranus/terminal/control",
		payload: `{"sessionId":"wrong-token","type":"create","token":"guess"}`,
	}, "uranus/terminal", m)
	if _, err := m.GetSession("wrong-token"); err == nil {
		t.Fatal("terminal session created with a wrong token")
	}

	handleMessage(nil, fakeMessage{
		topic:   "uranus/terminal/control",
		payload: `{"sessionId":"with-token","type":"create","token":"` + testToken + `"}`,
	}, "uranus/terminal", m)
	if _, err := m.GetSession("with-token"); err != nil {
		t.Fatalf("terminal session with the agent token was not created: %v", err)
	}
}

// installFakeKillall puts a killall first on PATH that only records its calls,
// so the test can tell whether Ctrl+C reached for system-wide killall.
func installFakeKillall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "killall.calls")
	script := "#!/bin/sh\necho \"$@\" >> '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "killall"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

// isolateHome keeps the shells started by a test away from the user's home
// directory (rc files, history).
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BASH_SILENCE_DEPRECATION_WARNING", "1")
}

// closeAndWait closes the session and waits for its shell to exit, so that it
// is gone (history written) before the test's temporary HOME is removed.
func closeAndWait(t *testing.T, s *Session) {
	t.Helper()
	s.Close()
	select {
	case <-s.shell.Exited():
	case <-time.After(5 * time.Second):
		t.Errorf("shell %d did not exit", s.shell.Pid())
	}
}

func testShell(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	isolateHome(t)
	return "/bin/bash"
}

type sessionOutput struct {
	mu  sync.Mutex
	buf strings.Builder
}

func collectOutput(s *Session) *sessionOutput {
	out := &sessionOutput{}
	go func() {
		for {
			select {
			case data := <-s.Output:
				out.mu.Lock()
				out.buf.Write(data)
				out.mu.Unlock()
			case <-s.Done:
				return
			}
		}
	}()
	return out
}

func (o *sessionOutput) waitFor(re *regexp.Regexp, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		ok := re.MatchString(o.buf.String())
		o.mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func (o *sessionOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// Ctrl+C must reach only the session's foreground job. It used to run
// `killall -SIGINT ping curl wget ssh tail` on the whole machine.
func TestSessionCtrlCStaysInsideSession(t *testing.T) {
	shell := testShell(t)
	marker := installFakeKillall(t)

	s, err := newSession("ctrl-c", shell)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	t.Cleanup(func() { closeAndWait(t, s) })
	out := collectOutput(s)

	if err := s.SendInput([]byte("echo START_$((1+2)); sleep 30\r")); err != nil {
		t.Fatal(err)
	}
	if !out.waitFor(regexp.MustCompile(`START_3`), 5*time.Second) {
		t.Fatalf("command did not start, output:\n%s", out)
	}
	time.Sleep(300 * time.Millisecond)

	if err := s.SendInput([]byte{3}); err != nil {
		t.Fatal(err)
	}
	if err := s.SendInput([]byte("echo DONE_$((2+3))\r")); err != nil {
		t.Fatal(err)
	}
	if !out.waitFor(regexp.MustCompile(`DONE_5`), 5*time.Second) {
		t.Fatalf("Ctrl+C did not interrupt the foreground job, output:\n%s", out)
	}

	time.Sleep(300 * time.Millisecond)
	if calls, err := os.ReadFile(marker); err == nil {
		t.Fatalf("Ctrl+C ran killall on the whole system: %s", calls)
	}
}

// Closed sessions must not leave zombie shells behind.
func TestClosedSessionShellIsReaped(t *testing.T) {
	shell := testShell(t)

	s, err := newSession("reap", shell)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	pid := s.shell.Pid()
	s.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("shell %d still in the process table after Close", pid)
}
