package wsterminal

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Manager handles multiple terminal sessions
type Manager struct {
	terminals map[string]*Terminal
	mu        sync.RWMutex
}

// NewManager creates a new terminal manager
func NewManager() *Manager {
	return &Manager{
		terminals: make(map[string]*Terminal),
	}
}

// CreateTerminal creates a new terminal session
func (m *Manager) CreateTerminal(conn *websocket.Conn, shell string) (*Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate session ID
	sessionID := fmt.Sprintf("term-%d", time.Now().UnixNano())

	// Create terminal
	terminal, err := NewTerminal(sessionID, conn, shell)
	if err != nil {
		return nil, fmt.Errorf("failed to create terminal: %v", err)
	}

	// Store terminal
	m.terminals[sessionID] = terminal
	log.Printf("[WS Terminal Manager] Terminal created: %s", sessionID)

	return terminal, nil
}

// GetTerminal retrieves a terminal session by ID
func (m *Manager) GetTerminal(sessionID string) (*Terminal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	terminal, exists := m.terminals[sessionID]
	if !exists {
		return nil, fmt.Errorf("terminal session not found: %s", sessionID)
	}

	return terminal, nil
}

// CloseTerminal closes a terminal session
func (m *Manager) CloseTerminal(sessionID string) error {
	m.mu.Lock()
	terminal, exists := m.terminals[sessionID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("terminal session not found: %s", sessionID)
	}

	delete(m.terminals, sessionID)
	m.mu.Unlock()

	terminal.Close()
	log.Printf("[WS Terminal Manager] Terminal closed: %s", sessionID)

	return nil
}

// CloseAll closes all terminal sessions
func (m *Manager) CloseAll() {
	m.mu.Lock()
	terminals := make([]*Terminal, 0, len(m.terminals))
	for _, terminal := range m.terminals {
		terminals = append(terminals, terminal)
	}
	m.terminals = make(map[string]*Terminal)
	m.mu.Unlock()

	for _, terminal := range terminals {
		terminal.Close()
	}

	log.Printf("[WS Terminal Manager] All terminals closed")
}

// ListTerminals lists all terminal sessions
func (m *Manager) ListTerminals() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]string, 0, len(m.terminals))
	for id := range m.terminals {
		sessions = append(sessions, id)
	}

	return sessions
}