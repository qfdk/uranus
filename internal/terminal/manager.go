// internal/terminal/manager.go
package terminal

import (
	"errors"
	"log"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

var (
	// ErrSessionNotFound 表示找不到指定会话ID的终端
	ErrSessionNotFound = errors.New("终端会话不存在")

	// ErrSessionAlreadyExists 表示会话ID已存在
	ErrSessionAlreadyExists = errors.New("终端会话已存在")
)

// Manager 管理所有终端会话
type Manager struct {
	terminals  map[string]*Terminal
	mutex      sync.RWMutex
	mqttClient mqtt.Client
}

// NewManager 创建一个新的终端管理器
func NewManager(mqttClient mqtt.Client) *Manager {
	return &Manager{
		terminals:  make(map[string]*Terminal),
		mqttClient: mqttClient,
	}
}

// CreateTerminal 创建一个新的终端会话
func (m *Manager) CreateTerminal(shell string) (string, error) {
	// 生成唯一会话ID
	sessionID := uuid.New().String()

	// 创建终端
	term, err := NewTerminal(sessionID, m.mqttClient, shell)
	if err != nil {
		return "", err
	}

	// 存储终端实例
	m.mutex.Lock()
	m.terminals[sessionID] = term
	m.mutex.Unlock()

	log.Printf("[终端管理器] 创建新会话: %s", sessionID)
	return sessionID, nil
}

// GetTerminal 获取终端会话
func (m *Manager) GetTerminal(sessionID string) (*Terminal, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	term, exists := m.terminals[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	return term, nil
}

// CloseTerminal 关闭终端会话
func (m *Manager) CloseTerminal(sessionID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	term, exists := m.terminals[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	// 关闭终端
	err := term.Close()
	if err != nil {
		log.Printf("[终端管理器] 关闭会话 %s 时出错: %v", sessionID, err)
	}

	// 从管理器中移除
	delete(m.terminals, sessionID)
	log.Printf("[终端管理器] 已关闭会话: %s", sessionID)

	return nil
}

// ResizeTerminal 调整终端窗口大小
func (m *Manager) ResizeTerminal(sessionID string, rows, cols uint16) error {
	term, err := m.GetTerminal(sessionID)
	if err != nil {
		return err
	}

	return term.Resize(rows, cols)
}

// ListSessions 列出所有活动的终端会话
func (m *Manager) ListSessions() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	sessions := make([]string, 0, len(m.terminals))
	for sessionID := range m.terminals {
		sessions = append(sessions, sessionID)
	}

	return sessions
}

// CloseAll 关闭所有终端会话
func (m *Manager) CloseAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for sessionID, term := range m.terminals {
		log.Printf("[终端管理器] 关闭会话: %s", sessionID)
		term.Close()
	}

	// 清空终端映射
	m.terminals = make(map[string]*Terminal)
}
