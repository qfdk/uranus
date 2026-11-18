package mqtty

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"uranus/internal/ptyshell"
)

// Session 终端会话
type Session struct {
	ID        string
	Created   time.Time
	Input     chan []byte
	Output    chan []byte
	Done      chan struct{}
	shell     *ptyshell.Shell
	closeOnce sync.Once
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	// 添加会话清理机制
	cleanupTicker    *time.Ticker
	stopCleanup      chan struct{}
	sessionTimeout   time.Duration
	lastActivityMap  map[string]time.Time
	activityMu       sync.RWMutex
	cleanupStopped   bool
	cleanupStoppedMu sync.Mutex
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions:        make(map[string]*Session),
		stopCleanup:     make(chan struct{}),
		sessionTimeout:  10 * time.Minute, // 10分钟无活动超时
		lastActivityMap: make(map[string]time.Time),
	}

	// 启动定期清理
	sm.startCleanupRoutine()
	return sm
}

// startCleanupRoutine 启动定期清理协程
func (m *SessionManager) startCleanupRoutine() {
	m.cleanupTicker = time.NewTicker(2 * time.Minute) // 每2分钟检查一次

	go func() {
		log.Printf("[MQTTY] 启动会话清理协程，超时时间: %v", m.sessionTimeout)
		for {
			select {
			case <-m.cleanupTicker.C:
				m.cleanupExpiredSessions()
			case <-m.stopCleanup:
				m.cleanupTicker.Stop()
				log.Printf("[MQTTY] 会话清理协程已停止")
				return
			}
		}
	}()
}

// cleanupExpiredSessions 清理过期会话
func (m *SessionManager) cleanupExpiredSessions() {
	now := time.Now()
	var expiredSessions []string
	var orphanedActivities []string

	m.activityMu.RLock()
	m.mu.RLock()

	for sessionID, lastActivity := range m.lastActivityMap {
		if now.Sub(lastActivity) > m.sessionTimeout {
			// 检查会话是否仍然存在
			if _, exists := m.sessions[sessionID]; exists {
				expiredSessions = append(expiredSessions, sessionID)
			} else {
				// 会话已不存在，但活动记录还在，标记为孤立记录
				orphanedActivities = append(orphanedActivities, sessionID)
			}
		} else {
			// 检查是否有会话已删除但活动记录仍存在的情况
			if _, exists := m.sessions[sessionID]; !exists {
				orphanedActivities = append(orphanedActivities, sessionID)
			}
		}
	}

	m.mu.RUnlock()
	m.activityMu.RUnlock()

	// 清理过期会话
	for _, sessionID := range expiredSessions {
		log.Printf("[MQTTY] 清理过期会话: %s", sessionID)
		m.CloseSession(sessionID)
	}

	// 清理孤立的活动记录
	if len(orphanedActivities) > 0 {
		m.activityMu.Lock()
		for _, sessionID := range orphanedActivities {
			delete(m.lastActivityMap, sessionID)
		}
		m.activityMu.Unlock()
		log.Printf("[MQTTY] 清理了 %d 个孤立的活动记录", len(orphanedActivities))
	}

	if len(expiredSessions) > 0 {
		log.Printf("[MQTTY] 本次清理了 %d 个过期会话", len(expiredSessions))
	}
}

// updateSessionActivity 更新会话活动时间
func (m *SessionManager) updateSessionActivity(sessionID string) {
	m.activityMu.Lock()
	m.lastActivityMap[sessionID] = time.Now()
	m.activityMu.Unlock()
}

// isSessionClosed 检查会话是否已关闭
func isSessionClosed(session *Session) bool {
	select {
	case <-session.Done:
		return true
	default:
		return false
	}
}

// CreateSession 创建新会话
func (m *SessionManager) CreateSession(sessionID, shell string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查会话是否已存在，如果存在则先关闭旧会话
	if session, exists := m.sessions[sessionID]; exists {
		log.Printf("[MQTTY] 会话ID已存在: %s，关闭旧会话并创建新会话", sessionID)
		// 关闭旧会话（不需要锁，因为已经在锁内）
		session.Close()
		delete(m.sessions, sessionID)

		// 清理活动记录
		m.activityMu.Lock()
		delete(m.lastActivityMap, sessionID)
		m.activityMu.Unlock()
	}

	session, err := newSession(sessionID, shell)
	if err != nil {
		return fmt.Errorf("创建会话失败: %v", err)
	}

	m.sessions[sessionID] = session

	// 记录活动时间
	m.updateSessionActivity(sessionID)

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

	// 清理活动记录
	m.activityMu.Lock()
	delete(m.lastActivityMap, sessionID)
	m.activityMu.Unlock()

	session.Close()
	log.Printf("[MQTTY] 会话已关闭: %s", sessionID)

	return nil
}

// CloseAll 关闭所有会话
func (m *SessionManager) CloseAll() {
	// 安全地停止清理协程
	m.cleanupStoppedMu.Lock()
	if !m.cleanupStopped {
		close(m.stopCleanup)
		m.cleanupStopped = true
	}
	m.cleanupStoppedMu.Unlock()

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	// 清理所有活动记录
	m.activityMu.Lock()
	m.lastActivityMap = make(map[string]time.Time)
	m.activityMu.Unlock()

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
	env := append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\e[32m\\]\\u@\\h:\\[\\e[33m\\]\\w\\[\\e[0m\\]\\$ ")
	sh, err := ptyshell.Start(shell, env, 24, 80)
	if err != nil {
		return nil, fmt.Errorf("启动命令失败: %v", err)
	}
	log.Printf("[MQTTY] 成功创建伪终端，PID: %d", sh.Pid())

	session := &Session{
		ID:      id,
		Created: time.Now(),
		Input:   make(chan []byte, 200),
		Output:  make(chan []byte, 200),
		Done:    make(chan struct{}),
		shell:   sh,
	}

	go session.writeInput()
	go session.readOutput()
	go func() {
		// shell 自己退出（例如用户输入 exit）时结束会话
		select {
		case <-sh.Exited():
			session.Close()
		case <-session.Done:
		}
	}()

	return session, nil
}

// writeInput 把输入写入 PTY。Ctrl+C（0x03）也按普通输入写入，由终端驱动发给前台进程。
func (s *Session) writeInput() {
	for {
		select {
		case input := <-s.Input:
			if _, err := s.shell.PTY().Write(input); err != nil {
				log.Printf("[MQTTY] 写入PTY错误: %v", err)
			}
		case <-s.Done:
			return
		}
	}
}

// readOutput 把 PTY 输出转到 Output 通道，PTY 关闭或 shell 退出时结束会话
func (s *Session) readOutput() {
	defer s.Close()

	buf := make([]byte, 16384)
	for {
		n, err := s.shell.PTY().Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case s.Output <- data:
			case <-s.Done:
				return
			default:
				log.Printf("[MQTTY] 输出缓冲区已满，丢弃数据")
			}
		}
		if err != nil {
			return
		}
	}
}

// Close 关闭会话：挂断终端，shell 及其作业收到 SIGHUP，未退出的进程组随后被强制结束
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		log.Printf("[MQTTY] 关闭会话: %s", s.ID)
		close(s.Done)
		s.shell.Close()
	})
}

// SendInput 发送输入
func (s *Session) SendInput(data []byte) error {
	select {
	case <-s.Done:
		return errors.New("会话已关闭")
	default:
	}

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
	return s.shell.Resize(rows, cols)
}
