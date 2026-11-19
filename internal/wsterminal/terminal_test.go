package wsterminal

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func prepareShellEnv(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash not available")
	}
	t.Setenv("HOME", t.TempDir()) // keep the user's rc files out of the session
	t.Setenv("BASH_SILENCE_DEPRECATION_WARNING", "1")
}

// dialTerminal connects to a terminal opened through m and collects everything
// the server sends.
func dialTerminal(t *testing.T, m *Manager) (*websocket.Conn, *clientOutput) {
	t.Helper()
	conn, _ := connectTerminal(t, m)

	out := &clientOutput{}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			out.mu.Lock()
			out.buf.Write(data)
			out.mu.Unlock()
		}
	}()
	return conn, out
}

// connectTerminal starts a server that opens a terminal per WebSocket
// connection through m and connects to it, without reading anything.
func connectTerminal(t *testing.T, m *Manager) (*websocket.Conn, *Terminal) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	terminals := make(chan *Terminal, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		term, err := m.CreateTerminal(conn, "")
		if err != nil {
			conn.Close()
			return
		}
		terminals <- term
		term.Start()
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	select {
	case term := <-terminals:
		// Let the shell exit (bash writes its history into HOME) before HOME is removed.
		t.Cleanup(func() {
			term.Close()
			select {
			case <-term.shell.Exited():
			case <-time.After(5 * time.Second):
				t.Errorf("shell %d did not exit", term.shell.Pid())
			}
		})
		return conn, term
	case <-time.After(5 * time.Second):
		t.Fatal("terminal was not created")
		return nil, nil
	}
}

type clientOutput struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (o *clientOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func (o *clientOutput) waitFor(re *regexp.Regexp, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if re.MatchString(o.String()) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func sendInput(t *testing.T, conn *websocket.Conn, input string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(input)); err != nil {
		t.Fatalf("send %q: %v", input, err)
	}
}

// Ctrl+C from the browser must only interrupt the session's foreground job.
// It used to run `killall -TERM ping dd nc cat` on the whole machine.
func TestCtrlCStaysInsideSession(t *testing.T) {
	prepareShellEnv(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "killall.calls")
	fake := "#!/bin/sh\necho \"$@\" >> '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "killall"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := NewManager()
	defer m.CloseAll()
	conn, out := dialTerminal(t, m)

	sendInput(t, conn, "echo START_$((1+2)); sleep 30\r")
	if !out.waitFor(regexp.MustCompile(`START_3`), 5*time.Second) {
		t.Fatalf("command did not start, output:\n%s", out)
	}
	time.Sleep(300 * time.Millisecond)

	sendInput(t, conn, "\x03")
	sendInput(t, conn, "echo DONE_$((2+3))\r")
	if !out.waitFor(regexp.MustCompile(`DONE_5`), 5*time.Second) {
		t.Fatalf("Ctrl+C did not interrupt the foreground job, output:\n%s", out)
	}

	time.Sleep(300 * time.Millisecond)
	if calls, err := os.ReadFile(marker); err == nil {
		t.Fatalf("Ctrl+C ran killall on the whole system: %s", calls)
	}
}

// Pong replies and terminal output are written from different goroutines;
// unsynchronized writes make gorilla/websocket panic and crash the process.
func TestPingsDuringHeavyOutput(t *testing.T) {
	prepareShellEnv(t)
	m := NewManager()
	defer m.CloseAll()
	conn, out := dialTerminal(t, m)

	sendInput(t, conn, "i=0; while [ $i -lt 20000 ]; do echo line-$i-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; i=$((i+1)); done; echo FLOOD_$((2*3))\r")
	done := regexp.MustCompile(`FLOOD_6`)
	deadline := time.Now().Add(60 * time.Second)
	for !done.MatchString(out.String()) {
		if time.Now().After(deadline) {
			t.Fatal("output flood did not finish")
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

// Closed terminals must be dropped from the manager instead of piling up.
func TestManagerForgetsClosedTerminals(t *testing.T) {
	prepareShellEnv(t)
	m := NewManager()
	defer m.CloseAll()
	conn, out := dialTerminal(t, m)

	sendInput(t, conn, "echo READY_$((4+5))\r")
	if !out.waitFor(regexp.MustCompile(`READY_9`), 5*time.Second) {
		t.Fatalf("terminal not ready, output:\n%s", out)
	}
	if n := len(m.ListTerminals()); n != 1 {
		t.Fatalf("expected 1 open terminal, got %d", n)
	}

	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(m.ListTerminals()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("closed terminal still tracked: %v", m.ListTerminals())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func shortKeepalive(t *testing.T) {
	t.Helper()
	period, wait := pingPeriod, pongWait
	pingPeriod, pongWait = 50*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { pingPeriod, pongWait = period, wait })
}

// A browser that went away without closing the connection (laptop closed,
// network gone) no longer answers pings: its root shell must be hung up.
func TestSilentClientIsDisconnected(t *testing.T) {
	prepareShellEnv(t)
	shortKeepalive(t)
	m := NewManager()
	defer m.CloseAll()
	_, term := connectTerminal(t, m) // nothing reads, so pings go unanswered

	select {
	case <-term.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal of a silent client is still open")
	}
}

// A browser that answers pings keeps its terminal while idle.
func TestAnsweringClientStaysConnected(t *testing.T) {
	prepareShellEnv(t)
	shortKeepalive(t)
	m := NewManager()
	defer m.CloseAll()
	conn, out := dialTerminal(t, m)

	time.Sleep(time.Second) // several pongWait periods without input
	sendInput(t, conn, "echo ALIVE_$((5+6))\r")
	if !out.waitFor(regexp.MustCompile(`ALIVE_11`), 5*time.Second) {
		t.Fatalf("idle terminal was closed, output:\n%s", out)
	}
}
