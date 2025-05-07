package wsterminal

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// Terminal represents a terminal session with WebSocket connectivity
type Terminal struct {
	ID        string
	Cmd       *exec.Cmd
	Pty       *os.File
	WsConn    *websocket.Conn
	Done      chan struct{}
	closeOnce sync.Once
	rows      uint16
	cols      uint16
}

// NewTerminal creates a new terminal session
func NewTerminal(id string, conn *websocket.Conn, shell string) (*Terminal, error) {
	log.Printf("[WS Terminal] Creating new terminal with ID: %s", id)

	if shell == "" {
		shell = getDefaultShell()
		log.Printf("[WS Terminal] Using default shell: %s", shell)
	}

	// Verify shell exists
	_, err := os.Stat(shell)
	if err != nil {
		log.Printf("[WS Terminal] Error with shell path %s: %v", shell, err)
		// Try to fallback to a basic shell if specified one doesn't exist
		if shell != "/bin/sh" {
			log.Printf("[WS Terminal] Falling back to /bin/sh")
			shell = "/bin/sh"
			// Check if /bin/sh exists
			_, err = os.Stat(shell)
			if err != nil {
				return nil, fmt.Errorf("fallback shell /bin/sh not available: %v", err)
			}
		} else {
			return nil, fmt.Errorf("shell not available: %v", err)
		}
	}

	// Create command
	log.Printf("[WS Terminal] Creating command with shell: %s", shell)
	cmd := exec.Command(shell)

	// Initialize environment variables
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\033[1;31m\\]\\u\\[\\033[1;33m\\]@\\[\\033[1;32m\\]\\h:\\[\\033[1;34m\\][\\w]\\$\\[\\033[0m\\] ")

	// Configure process group so we can send signals to the entire process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Create PTY
	log.Printf("[WS Terminal] Creating PTY...")
	ptmx, tty, err := pty.Open()
	if err != nil {
		log.Printf("[WS Terminal] Failed to open PTY: %v", err)
		return nil, fmt.Errorf("failed to open PTY: %v", err)
	}

	// Set command's stdin/stdout/stderr
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	// Start command
	log.Printf("[WS Terminal] Creating pseudo-terminal, Shell: %s, OS: %s", shell, runtime.GOOS)
	if err := cmd.Start(); err != nil {
		log.Printf("[WS Terminal] Failed to start command: %v", err)
		tty.Close()
		ptmx.Close()
		return nil, fmt.Errorf("failed to start command: %v", err)
	}

	// Close TTY file descriptor, child process has inherited it
	tty.Close()

	log.Printf("[WS Terminal] Successfully created pseudo-terminal, PID: %d", cmd.Process.Pid)

	// Set default terminal size
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	terminal := &Terminal{
		ID:     id,
		Cmd:    cmd,
		Pty:    ptmx,
		WsConn: conn,
		Done:   make(chan struct{}),
		rows:   24,
		cols:   80,
	}

	return terminal, nil
}

// Start begins the I/O handling for the terminal
func (t *Terminal) Start() {
	log.Printf("[WS Terminal] Starting terminal I/O for session: %s", t.ID)

	// Handle WebSocket to PTY (user input)
	go func() {
		defer func() {
			log.Printf("[WS Terminal] WebSocket->PTY handler exiting for session: %s", t.ID)
			t.Close()
		}()

		for {
			log.Printf("[WS Terminal] Waiting for WebSocket message...")
			messageType, p, err := t.WsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[WS Terminal] Error reading from WebSocket: %v", err)
				} else {
					log.Printf("[WS Terminal] WebSocket closed: %v", err)
				}
				break
			}

			log.Printf("[WS Terminal] Received message type: %d, length: %d", messageType, len(p))

			if messageType == websocket.TextMessage {
				// Handle control messages (encoded as JSON)
				if len(p) > 0 && p[0] == '{' {
					log.Printf("[WS Terminal] Detected JSON message, handling as control")
					handleControlMessage(t, p)
					continue
				}
				log.Printf("[WS Terminal] Text message, forwarding to PTY")
			}

			// 检查是否是Ctrl+C (ASCII 3)
			if len(p) == 1 && p[0] == 3 {
				log.Printf("[WS Terminal] Detected Ctrl+C, handling interrupt")
				
				// 1. 调用SendInterrupt方法发送中断信号
				err := t.SendInterrupt()
				if err != nil {
					log.Printf("[WS Terminal] Error sending interrupt: %v", err)
				}
				
				// 2. 写入标准的终端中断字符，同时确保shell能看到这个信号
				// 这对于进程自己处理中断信号很重要
			}

			// Write to PTY
			log.Printf("[WS Terminal] Writing %d bytes to PTY", len(p))
			if _, err := t.Pty.Write(p); err != nil {
				log.Printf("[WS Terminal] Error writing to PTY: %v", err)
				break
			}
		}
	}()

	// Handle PTY to WebSocket (terminal output)
	go func() {
		defer func() {
			log.Printf("[WS Terminal] PTY->WebSocket handler exiting for session: %s", t.ID)
			t.Close()
		}()

		buf := make([]byte, 16384) // 16KB buffer
		log.Printf("[WS Terminal] Starting PTY output reader")

		for {
			select {
			case <-t.Done:
				log.Printf("[WS Terminal] Terminal session done signal received")
				return
			default:
				// Read from PTY
				n, err := t.Pty.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("[WS Terminal] Error reading from PTY: %v", err)
					} else {
						log.Printf("[WS Terminal] PTY EOF reached")
					}
					return
				}

				// Write to WebSocket
				if n > 0 {
					log.Printf("[WS Terminal] Read %d bytes from PTY, writing to WebSocket", n)
					if err := t.WsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
						log.Printf("[WS Terminal] Error writing to WebSocket: %v", err)
						return
					}
				}
			}
		}
	}()

	// Wait for command to finish
	go func() {
		defer func() {
			log.Printf("[WS Terminal] Command watcher exiting for session: %s", t.ID)
			t.Close()
		}()

		log.Printf("[WS Terminal] Waiting for command to complete")
		err := t.Cmd.Wait()
		log.Printf("[WS Terminal] Command exited: %v", err)
	}()

	// Send initial message to client
	welcomeMsg := []byte("\r\nWelcome to WebSocket Terminal\r\n\r\n")
	t.WsConn.WriteMessage(websocket.BinaryMessage, welcomeMsg)

	// 在服务器端配置 Shell 环境，这样用户就不会看到这些命令执行过程
	time.Sleep(100 * time.Millisecond) // 确保 shell 已经准备好接收命令
	shellInitCmd := []byte("export TERM=xterm-256color && " +
		"PS1=\"\\[\\033[1;31m\\]\\u\\[\\033[1;33m\\]@\\[\\033[1;32m\\]\\h:\\[\\033[1;34m\\][\\w]\\$\\[\\033[0m\\] \" && " +
		"alias ls='ls --color' && " +
		"alias ll='ls -alF' && " +
		"clear\n")
	t.Pty.Write(shellInitCmd)
}

// Close terminates the terminal session
func (t *Terminal) Close() {
	t.closeOnce.Do(func() {
		log.Printf("[WS Terminal] Closing terminal: %s", t.ID)
		close(t.Done)

		// Close WebSocket with timeout handling
		if t.WsConn != nil {
			closeWSTimeout := 5 * time.Second
			wsCloseChan := make(chan bool, 1)
			
			// Setup a timeout for websocket closure
			go func() {
				t.WsConn.WriteControl(
					websocket.CloseMessage, 
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Terminal closed"),
					time.Now().Add(time.Second),
				)
				t.WsConn.Close()
				wsCloseChan <- true
			}()
			
			// Wait with timeout
			select {
			case <-wsCloseChan:
				log.Printf("[WS Terminal] WebSocket closed gracefully")
			case <-time.After(closeWSTimeout):
				log.Printf("[WS Terminal] WebSocket close timed out")
			}
		}

		// Terminate process first - this is important to do before closing PTY
		// to avoid orphaned processes
		if t.Cmd != nil && t.Cmd.Process != nil {
			pid := t.Cmd.Process.Pid
			log.Printf("[WS Terminal] Terminating process: %d", pid)
			
			// Try to terminate process group
			terminateSuccess := false
			if pgid, err := syscall.Getpgid(pid); err == nil {
				// First, try to terminate gracefully
				if err := syscall.Kill(-pgid, syscall.SIGINT); err == nil {
					log.Printf("[WS Terminal] Sent SIGINT to process group: %d", -pgid)
					
					// Give process a chance to terminate gracefully
					terminateSuccess = waitForProcessExit(pid, 500*time.Millisecond)
					if terminateSuccess {
						log.Printf("[WS Terminal] Process terminated with SIGINT")
					}
				}
				
				// If still running, try SIGTERM
				if !terminateSuccess {
					if err := syscall.Kill(-pgid, syscall.SIGTERM); err == nil {
						log.Printf("[WS Terminal] Sent SIGTERM to process group: %d", -pgid)
						terminateSuccess = waitForProcessExit(pid, 500*time.Millisecond)
						if terminateSuccess {
							log.Printf("[WS Terminal] Process terminated with SIGTERM")
						}
					}
				}
				
				// Finally, if all else fails, use SIGKILL
				if !terminateSuccess {
					if err := syscall.Kill(-pgid, syscall.SIGKILL); err == nil {
						log.Printf("[WS Terminal] Sent SIGKILL to process group: %d", -pgid)
						waitForProcessExit(pid, 500*time.Millisecond)
					}
				}
			} else {
				// If we can't get process group ID, send signals directly to process
				log.Printf("[WS Terminal] Failed to get process group ID: %v, sending signals directly to process", err)
				t.Cmd.Process.Signal(syscall.SIGINT)
				if !waitForProcessExit(pid, 300*time.Millisecond) {
					t.Cmd.Process.Signal(syscall.SIGTERM)
					if !waitForProcessExit(pid, 300*time.Millisecond) {
						t.Cmd.Process.Kill()
					}
				}
			}
		}
		
		// Close PTY after process termination
		if t.Pty != nil {
			err := t.Pty.Close()
			if err != nil {
				log.Printf("[WS Terminal] Error closing PTY: %v", err)
			} else {
				log.Printf("[WS Terminal] PTY closed successfully")
			}
		}
	})
}

// waitForProcessExit checks if a process has exited within the given timeout
func waitForProcessExit(pid int, timeout time.Duration) bool {
	exitChan := make(chan bool, 1)
	
	// Check process in goroutine
	go func() {
		for {
			// Use Signal(0) to check if process exists without sending a signal
			process, err := os.FindProcess(pid)
			if err != nil || process.Signal(syscall.Signal(0)) != nil {
				exitChan <- true
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	
	// Wait with timeout
	select {
	case <-exitChan:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Resize resizes the terminal
func (t *Terminal) Resize(rows, cols uint16) error {
	if t.Pty == nil {
		return fmt.Errorf("PTY not initialized")
	}

	t.rows = rows
	t.cols = cols

	return pty.Setsize(t.Pty, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// getDefaultShell returns the default shell
func getDefaultShell() string {
	// Prefer bash
	shells := []string{"/bin/bash", "/bin/sh", "/bin/zsh"}

	if runtime.GOOS == "darwin" {
		// On macOS, prefer bash over zsh for stability
		shells = []string{"/bin/bash", "/bin/sh", "/bin/zsh"}

		// Check executable permissions
		for _, shell := range shells {
			if info, err := os.Stat(shell); err == nil {
				// Ensure file exists and is executable
				if info.Mode()&0111 != 0 {
					log.Printf("[WS Terminal] Using shell: %s", shell)
					return shell
				}
			}
		}

		// If all shells are unavailable, try /bin/sh
		log.Printf("[WS Terminal] Warning: All preferred shells unavailable, using /bin/sh")
		return "/bin/sh"
	}

	// Other systems
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			log.Printf("[WS Terminal] Using shell: %s", shell)
			return shell
		}
	}

	// Fallback shell
	log.Printf("[WS Terminal] Warning: All shells unavailable, using /bin/sh")
	return "/bin/sh"
}

// SendInterrupt sends interrupt signals to the process group
func (t *Terminal) SendInterrupt() error {
	if t.Cmd == nil || t.Cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
	if err != nil {
		return fmt.Errorf("failed to get process group ID: %v", err)
	}

	// 1. 首先尝试向进程组发送SIGINT
	log.Printf("[WS Terminal] Sending SIGINT to process group: %d", -pgid)
	err = syscall.Kill(-pgid, syscall.SIGINT)
	if err != nil {
		log.Printf("[WS Terminal] Failed to send SIGINT: %v", err)
	}

	// 2. 使用goroutine在短暂延迟后发送SIGTERM，如果进程仍在运行
	go func() {
		// 等待一小段时间看SIGINT是否生效
		time.Sleep(100 * time.Millisecond)

		// 再次检查进程是否仍在运行
		if processIsRunning(t.Cmd.Process.Pid) {
			log.Printf("[WS Terminal] Process still running after SIGINT, sending SIGTERM to group: %d", -pgid)
			if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
				log.Printf("[WS Terminal] Failed to send SIGTERM: %v", err)
			}

			// 为ping和一些常见的顽固进程执行killall
			if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
				for _, procName := range []string{"ping", "dd", "nc", "cat"} {
					killCmd := exec.Command("killall", "-TERM", procName)
					_ = killCmd.Run()
				}
			}
		}
	}()

	return nil
}

// 检查进程是否仍在运行
func processIsRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// 在Unix-like系统上，os.FindProcess总是成功的，需要发送信号0来检查进程是否存在
	if runtime.GOOS != "windows" {
		err = process.Signal(syscall.Signal(0))
		return err == nil
	}

	// Windows平台，进程存在则返回nil
	return true
}
