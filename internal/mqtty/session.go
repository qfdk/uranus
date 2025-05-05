package mqtty

import (
	"errors"
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
)

// Session 终端会话
type Session struct {
	ID        string
	Cmd       *exec.Cmd
	PTY       *os.File
	Created   time.Time
	Input     chan []byte
	Output    chan []byte
	Done      chan struct{}
	closeOnce sync.Once
	rows      uint16
	cols      uint16
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// CreateSession 创建新会话
func (m *SessionManager) CreateSession(sessionID, shell string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查会话是否已存在
	if session, exists := m.sessions[sessionID]; exists {
		// 检查会话是否已关闭
		var isClosed bool
		select {
		case <-session.Done:
			isClosed = true
		default:
			isClosed = false
		}

		if isClosed {
			// 会话已关闭，重新创建
			log.Printf("[MQTT] 会话 %s 已关闭，重新创建", sessionID)
			newSession, err := newSession(sessionID, shell)
			if err != nil {
				return fmt.Errorf("创建会话失败: %v", err)
			}
			m.sessions[sessionID] = newSession
		} else {
			// 会话未关闭，可以复用
			log.Printf("[MQTT] 会话ID已存在: %s，复用该会话", sessionID)
		}
		return nil
	}

	if shell == "" {
		shell = getDefaultShell()
	}

	session, err := newSession(sessionID, shell)
	if err != nil {
		return fmt.Errorf("创建会话失败: %v", err)
	}

	m.sessions[sessionID] = session
	log.Printf("[MQTTY] 会话已创建: %s", sessionID)

	return nil
}

// GetSession 获取会话
func (m *SessionManager) GetSession(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, errors.New("会话不存在")
	}

	return session, nil
}

// CloseSession 关闭会话
func (m *SessionManager) CloseSession(sessionID string) error {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if !exists {
		m.mu.Unlock()
		return errors.New("会话不存在")
	}

	delete(m.sessions, sessionID)
	m.mu.Unlock()

	session.Close()
	log.Printf("[MQTTY] 会话已关闭: %s", sessionID)

	return nil
}

// CloseAll 关闭所有会话
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, session := range sessions {
		session.Close()
	}

	log.Printf("[MQTTY] 已关闭所有会话")
}

// ListSessions 列出所有会话
func (m *SessionManager) ListSessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		sessions = append(sessions, id)
	}

	return sessions
}

// newSession 创建新的会话实例
func newSession(id, shell string) (*Session, error) {
	cmd := exec.Command(shell)

	// 初始化环境变量
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\e[32m\\]\\u@\\h:\\[\\e[33m\\]\\w\\[\\e[0m\\]\\$ ")

	// 手动创建PTY master/slave对
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("无法打开PTY: %v", err)
	}

	// 设置命令的标准输入输出
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	// 将SysProcAttr设置为nil，完全不使用Setctty
	cmd.SysProcAttr = nil

	// 创建PTY前添加日志
	log.Printf("[MQTTY] 正在创建伪终端，Shell: %s, OS: %s", shell, runtime.GOOS)

	// 启动命令
	if err := cmd.Start(); err != nil {
		tty.Close()
		ptmx.Close()
		return nil, fmt.Errorf("启动命令失败: %v", err)
	}

	// 关闭TTY的文件描述符，此时子进程已经继承了它
	tty.Close()

	log.Printf("[MQTTY] 成功创建伪终端，PID: %d", cmd.Process.Pid)

	// 默认终端大小
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
	})

	session := &Session{
		ID:      id,
		Cmd:     cmd,
		PTY:     ptmx,
		Created: time.Now(),
		Input:   make(chan []byte, 100),
		Output:  make(chan []byte, 100),
		Done:    make(chan struct{}),
		rows:    24,
		cols:    80,
	}

	// 启动I/O处理
	go session.handleIO()

	return session, nil
}

// handleIO 处理会话I/O
func (s *Session) handleIO() {
	// 处理输入
	go func() {
		for {
			select {
			case input := <-s.Input:
				if s.PTY != nil {
					_, err := s.PTY.Write(input)
					if err != nil {
						log.Printf("[MQTTY] 写入PTY错误: %v", err)
					}
				}
			case <-s.Done:
				return
			}
		}
	}()

	// 处理输出
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-s.Done:
				return
			default:
				if s.PTY == nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				n, err := s.PTY.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("[MQTTY] 读取PTY错误: %v", err)
					}
					s.Close()
					return
				}

				if n > 0 {
					data := make([]byte, n)
					copy(data, buf[:n])

					select {
					case s.Output <- data:
					default:
						log.Printf("[MQTTY] 输出缓冲区已满，丢弃数据")
					}
				}
			}
		}
	}()
}

// Close 关闭会话
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.Done)

		if s.PTY != nil {
			s.PTY.Close()
		}

		if s.Cmd != nil && s.Cmd.Process != nil {
			// 尝试终止进程组
			if pgid, err := syscall.Getpgid(s.Cmd.Process.Pid); err == nil {
				syscall.Kill(-pgid, syscall.SIGTERM)
				time.Sleep(100 * time.Millisecond)
				syscall.Kill(-pgid, syscall.SIGKILL)
			}

			s.Cmd.Process.Kill()
		}
	})
}

// SendInput 发送输入
func (s *Session) SendInput(data []byte) error {
	select {
	case s.Input <- data:
		return nil
	case <-s.Done:
		return errors.New("会话已关闭")
	default:
		return errors.New("输入缓冲区已满")
	}
}

// Resize 调整终端大小
func (s *Session) Resize(rows, cols uint16) error {
	if s.PTY == nil {
		return errors.New("PTY未初始化")
	}

	s.rows = rows
	s.cols = cols

	return pty.Setsize(s.PTY, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// getDefaultShell 获取默认shell
func getDefaultShell() string {
	// 优先使用 bash
	shells := []string{"/bin/bash", "/bin/sh", "/bin/zsh"}

	if runtime.GOOS == "darwin" {
		// macOS上首选 bash 而非 zsh，因为可能更稳定
		shells = []string{"/bin/bash", "/bin/sh", "/bin/zsh"}

		// 检查可执行权限
		for _, shell := range shells {
			if info, err := os.Stat(shell); err == nil {
				// 确保文件存在且可执行
				if info.Mode()&0111 != 0 {
					log.Printf("[MQTTY] 使用shell: %s", shell)
					return shell
				}
			}
		}

		// 所有shell均不可用时，尝试使用/bin/sh
		log.Printf("[MQTTY] 警告: 所有首选shell不可用，使用/bin/sh")
		return "/bin/sh"
	}

	// 其他系统
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			log.Printf("[MQTTY] 使用shell: %s", shell)
			return shell
		}
	}

	// 兜底shell
	log.Printf("[MQTTY] 警告: 所有shell不可用，使用/bin/sh")
	return "/bin/sh"
}
