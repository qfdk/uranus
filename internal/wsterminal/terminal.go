package wsterminal

import (
	"fmt"
	"log"
	"sync"
	"time"

	"uranus/internal/ptyshell"

	"github.com/gorilla/websocket"
)

// writeWait bounds how long a write may block on a slow or stalled client.
const writeWait = 10 * time.Second

// The server pings the browser every pingPeriod. A connection that stays
// silent for pongWait (no pong, no input) is gone, e.g. a laptop that was
// closed: the terminal is closed and its shell hung up instead of waiting for
// TCP to notice.
var (
	pingPeriod = 30 * time.Second
	pongWait   = 60 * time.Second
)

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

	sh, err := ptyshell.Start(shell, 24, 80)
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

	alive := func() error { return t.WsConn.SetReadDeadline(time.Now().Add(pongWait)) }
	_ = alive()
	t.WsConn.SetPongHandler(func(string) error { return alive() })

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
			_ = alive()

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

	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// WriteControl may run concurrently with the output writer.
				if err := t.WsConn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
					t.Close()
					return
				}
			case <-t.Done:
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
