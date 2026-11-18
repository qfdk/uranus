package wsterminal

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"uranus/internal/ptyshell"

	"github.com/gorilla/websocket"
)

// writeWait bounds how long a write may block on a slow or stalled client.
const writeWait = 10 * time.Second

// Terminal represents a terminal session with WebSocket connectivity
type Terminal struct {
	ID     string
	WsConn *websocket.Conn
	Done   chan struct{}
	shell  *ptyshell.Shell
	// gorilla/websocket allows only one writer at a time; concurrent writes panic.
	writeMu   sync.Mutex
	closeOnce sync.Once
}

// NewTerminal creates a new terminal session
func NewTerminal(id string, conn *websocket.Conn, shell string) (*Terminal, error) {
	log.Printf("[WS Terminal] Creating new terminal with ID: %s", id)

	env := append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\033[1;31m\\]\\u\\[\\033[1;33m\\]@\\[\\033[1;32m\\]\\h:\\[\\033[1;34m\\][\\w]\\$\\[\\033[0m\\] ")
	sh, err := ptyshell.Start(shell, env, 24, 80)
	if err != nil {
		log.Printf("[WS Terminal] Failed to start shell: %v", err)
		return nil, fmt.Errorf("failed to start shell: %v", err)
	}
	log.Printf("[WS Terminal] Successfully created pseudo-terminal, PID: %d", sh.Pid())

	return &Terminal{
		ID:     id,
		WsConn: conn,
		Done:   make(chan struct{}),
		shell:  sh,
	}, nil
}

// write sends one message to the client. Every WriteMessage must go through it.
func (t *Terminal) write(messageType int, data []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = t.WsConn.SetWriteDeadline(time.Now().Add(writeWait))
	return t.WsConn.WriteMessage(messageType, data)
}

// Start begins the I/O handling for the terminal
func (t *Terminal) Start() {
	log.Printf("[WS Terminal] Starting terminal I/O for session: %s", t.ID)

	go func() {
		defer func() {
			log.Printf("[WS Terminal] WebSocket->PTY handler exiting for session: %s", t.ID)
			t.Close()
		}()

		for {
			messageType, p, err := t.WsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[WS Terminal] Error reading from WebSocket: %v", err)
				}
				return
			}

			if messageType == websocket.TextMessage && len(p) > 0 && p[0] == '{' {
				handleControlMessage(t, p)
				continue
			}

			// Ctrl+C arrives as byte 0x03; the terminal turns it into SIGINT for the foreground job.
			if _, err := t.shell.PTY().Write(p); err != nil {
				log.Printf("[WS Terminal] Error writing to PTY: %v", err)
				return
			}
		}
	}()

	go func() {
		defer func() {
			log.Printf("[WS Terminal] PTY->WebSocket handler exiting for session: %s", t.ID)
			t.Close()
		}()

		buf := make([]byte, 16384)
		for {
			n, err := t.shell.PTY().Read(buf)
			if n > 0 {
				if err := t.write(websocket.BinaryMessage, buf[:n]); err != nil {
					log.Printf("[WS Terminal] Error writing to WebSocket: %v", err)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// End the session when the shell exits, e.g. after `exit`.
	go func() {
		select {
		case <-t.shell.Exited():
			log.Printf("[WS Terminal] Shell exited for session: %s", t.ID)
			t.Close()
		case <-t.Done:
		}
	}()

	// Send initial message to client
	_ = t.write(websocket.BinaryMessage, []byte("\r\nWelcome to WebSocket Terminal\r\n\r\n"))

	// 在服务器端配置 Shell 环境，这样用户就不会看到这些命令执行过程
	time.Sleep(100 * time.Millisecond) // 确保 shell 已经准备好接收命令
	shellInitCmd := []byte("export TERM=xterm-256color && " +
		"PS1=\"\\[\\033[1;31m\\]\\u\\[\\033[1;33m\\]@\\[\\033[1;32m\\]\\h:\\[\\033[1;34m\\][\\w]\\$\\[\\033[0m\\] \" && " +
		"alias ls='" + lsWithColor() + "' && " +
		"alias ll='ls -alF' && " +
		"clear\n")
	_, _ = t.shell.PTY().Write(shellInitCmd)
}

// lsWithColor returns an ls invocation with colors: BSD ls on macOS uses -G,
// and macOS releases before Ventura reject --color.
func lsWithColor() string {
	if runtime.GOOS == "darwin" {
		return "ls -G"
	}
	return "ls --color=auto"
}

// Close terminates the terminal session
func (t *Terminal) Close() {
	t.closeOnce.Do(func() {
		log.Printf("[WS Terminal] Closing terminal: %s", t.ID)
		close(t.Done)

		// WriteControl is safe to call concurrently with the other writers.
		_ = t.WsConn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Terminal closed"),
			time.Now().Add(time.Second),
		)
		_ = t.WsConn.Close()

		t.shell.Close()
	})
}

// Resize resizes the terminal
func (t *Terminal) Resize(rows, cols uint16) error {
	return t.shell.Resize(rows, cols)
}
