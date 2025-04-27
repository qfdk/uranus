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

	if _, exists := m.sessions[sessionID]; exists {
		return errors.New("会话ID已存在")
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

	// 根据不同操作系统设置进程属性
	if runtime.GOOS == "darwin" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Setsid:  true,
		}
	}

	// 创建PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

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
	shells := []string{"/bin/bash", "/bin/sh", "/bin/zsh"}

	if runtime.GOOS == "darwin" {
		shells = []string{"/bin/zsh", "/bin/bash", "/bin/sh"}
	}

	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	// 兜底shell
	return "/bin/sh"
}
