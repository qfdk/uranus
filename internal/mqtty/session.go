package mqtty

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

	if shell == "" {
		shell = getDefaultShell()
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
	cmd := exec.Command(shell)

	// 初始化环境变量
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\e[32m\\]\\u@\\h:\\[\\e[33m\\]\\w\\[\\e[0m\\]\\$ ")

	// 配置进程组，使得命令在自己的进程组中运行
	// 这样可以将信号发送到整个进程组，包括子进程
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // 设置进程组ID
	}

	// 手动创建PTY master/slave对
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("无法打开PTY: %v", err)
	}

	// 设置命令的标准输入输出
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

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
		Input:   make(chan []byte, 200),
		Output:  make(chan []byte, 200),
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
					} else {
						// 特殊处理如果输入是Ctrl+C，确保连续发送回车，帮助中断正在运行的命令
						if len(input) == 1 && input[0] == 3 {
							log.Printf("[MQTTY] 检测到Ctrl+C，向进程组发送SIGINT")
							// 向进程组发送SIGINT信号
							if s.Cmd != nil && s.Cmd.Process != nil {
								if pgid, err := syscall.Getpgid(s.Cmd.Process.Pid); err == nil {
									log.Printf("[MQTTY] 向进程组 %d 发送SIGINT", -pgid)
									if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
										log.Printf("[MQTTY] 向进程组发送SIGINT失败: %v", err)
									}
								}
							}
							// 短暂延迟后发送回车，帮助刷新提示符
							time.Sleep(100 * time.Millisecond)
							s.PTY.Write([]byte{13}) // 发送回车(CR)
						}
					}
				}
			case <-s.Done:
				return
			}
		}
	}()

	// 处理输出
	go func() {
		// 增大缓冲区大小以提高性能
		buf := make([]byte, 16384) // 增加到 16KB

		// 使用永久缓冲区来减少内存分配
		outputBuf := make([]byte, 0, 16384)

		for {
			// 简化选择逻辑，避免频繁的select
			if s.PTY == nil {
				// 检查是否需要退出
				select {
				case <-s.Done:
					return
				default:
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}

			// 使用最小延迟读取数据
			setReadDeadline(s.PTY, 100*time.Millisecond)
			n, err := s.PTY.Read(buf)

			// 检查是否需要退出
			select {
			case <-s.Done:
				return
			default:
				// 继续处理
			}

			// 处理读取错误
			if err != nil {
				// 读取超时不是错误，继续读取
				if isTimeoutError(err) {
					continue
				}

				// 其他错误处理
				if err != io.EOF {
					log.Printf("[MQTTY] 读取PTY错误: %v", err)
				}
				s.Close()
				return
			}

			if n > 0 {
				// 重用缓冲区，避免频繁分配
				outputBuf = outputBuf[:n]
				copy(outputBuf, buf[:n])

				// 非阻塞写入输出缓冲区
				select {
				case s.Output <- outputBuf:
					// 写入成功后需要分配新的内存
					outputBuf = make([]byte, 0, 8192)
				default:
					log.Printf("[MQTTY] 输出缓冲区已满，丢弃数据")
				}
			}
		}
	}()
}

// setReadDeadline 设置读取超时
// 注意：此函数在某些系统上可能不生效，因为 PTY 可能不支持设置超时
// 这种情况下程序会正常工作，但无法利用超时机制
// 程序需要考虑其他方式来处理 read 调用的阻塞问题
func setReadDeadline(file *os.File, timeout time.Duration) {
	// 如果文件支持 SetReadDeadline，尝试设置超时
	// 定义接口类型
	type timeoutSetter interface {
		SetReadDeadline(time.Time) error
	}

	// 先将file转换为interface{}，再转换为目标接口
	if fd, ok := interface{}(file).(timeoutSetter); ok {
		_ = fd.SetReadDeadline(time.Now().Add(timeout))
	}
}

// isTimeoutError 检查错误是否为超时错误
func isTimeoutError(err error) bool {
	// 此函数判断错误是否为超时错误
	if netErr, ok := err.(interface{ Timeout() bool }); ok {
		return netErr.Timeout()
	}
	// 没有Timeout方法，检查错误消息中是否包含"timeout"
	return err != nil && (strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline") ||
		strings.Contains(err.Error(), "temporarily unavailable"))
}

// Close 关闭会话
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		log.Printf("[MQTTY] 关闭会话: %s", s.ID)
		close(s.Done)

		if s.PTY != nil {
			s.PTY.Close()
			log.Printf("[MQTTY] 已关闭PTY")
		}

		if s.Cmd != nil && s.Cmd.Process != nil {
			log.Printf("[MQTTY] 尝试终止进程: %d", s.Cmd.Process.Pid)
			// 尝试终止进程组 - 使用较温和的终止流程
			if pgid, err := syscall.Getpgid(s.Cmd.Process.Pid); err == nil {
				// 先发送中断信号
				_ = syscall.Kill(-pgid, syscall.SIGINT)
				log.Printf("[MQTTY] 发送SIGINT到进程组: %d", -pgid)
				time.Sleep(100 * time.Millisecond)

				// 如果进程仍在运行，发送终止信号
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
				log.Printf("[MQTTY] 发送SIGTERM到进程组: %d", -pgid)
				time.Sleep(100 * time.Millisecond)

				// 最后发送强制终止信号
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				log.Printf("[MQTTY] 发送SIGKILL到进程组: %d", -pgid)
			} else {
				// 如果无法获取进程组ID，尝试直接向进程发送信号
				log.Printf("[MQTTY] 获取进程组ID失败: %v，尝试直接向进程发送信号", err)
				_ = s.Cmd.Process.Signal(syscall.SIGINT)
				time.Sleep(100 * time.Millisecond)
				_ = s.Cmd.Process.Signal(syscall.SIGTERM)
				time.Sleep(100 * time.Millisecond)
				s.Cmd.Process.Kill()
			}
		}
	})
}

// SendInput 发送输入
func (s *Session) SendInput(data []byte) error {
	// 检查是否是 Ctrl+C (ASCII 3, ETX - End of Text)
	if len(data) == 1 && data[0] == 3 {
		// 在这里特殊处理Ctrl+C
		if s.Cmd != nil && s.Cmd.Process != nil {
			log.Printf("[MQTTY] 检测到Ctrl+C，向进程组发送SIGINT")

			// 1. 发送字符给PTY
			_, _ = s.PTY.Write([]byte{3})
			log.Printf("[MQTTY] 向PTY发送Ctrl+C字符")

			// 2. 向进程组发送中断信号
			if pgid, err := syscall.Getpgid(s.Cmd.Process.Pid); err == nil {
				log.Printf("[MQTTY] 向进程组 %d 发送SIGINT", -pgid)
				if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
					log.Printf("[MQTTY] 向进程组发送SIGINT失败: %v", err)
				}
			} else {
				log.Printf("[MQTTY] 获取进程组ID失败: %v", err)
			}

			// 3. 强化进程清理 - 尝试终止可能的stuck进程
			if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
				// 尝试终止常见的可能卡住的进程
				stuckProcesses := []string{"ping", "curl", "wget", "ssh", "tail"}
				for _, proc := range stuckProcesses {
					killCmd := exec.Command("killall", "-SIGINT", proc)
					if err := killCmd.Run(); err == nil {
						log.Printf("[MQTTY] 已终止可能卡住的进程: %s", proc)
					}
				}
			}

			return nil
		}
	}

	// 正常处理其他输入
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

// GetDefaultShell 获取默认shell (exported version)
func GetDefaultShell() string {
	return getDefaultShell()
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
