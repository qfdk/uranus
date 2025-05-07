package wsterminal

import (
	"sync"
)

var (
	// globalManager is the global terminal manager instance
	globalManager *Manager
	
	// initOnce ensures the globalManager is initialized only once
	initOnce sync.Once
	
	// managerMutex protects access to the globalManager
	managerMutex sync.RWMutex
)

// GetGlobalManager returns the global terminal manager instance
func GetGlobalManager() *Manager {
	managerMutex.RLock()
	if globalManager == nil {
		managerMutex.RUnlock()
		managerMutex.Lock()
		defer managerMutex.Unlock()
		
		if globalManager == nil {
			globalManager = NewManager()
		}
		return globalManager
	}
	defer managerMutex.RUnlock()
	return globalManager
}

// InitGlobalManager initializes the global terminal manager
func InitGlobalManager() *Manager {
	managerMutex.Lock()
	defer managerMutex.Unlock()
	
	initOnce.Do(func() {
		globalManager = NewManager()
	})
	
	return globalManager
}