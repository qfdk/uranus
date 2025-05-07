package controllers

import (
	"log"
	"net/http"
	"uranus/internal/config"
	"uranus/internal/wsterminal"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocket upgrader configuration
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// WebSocketTerminalHandler handles WebSocket connections for the terminal
func WebSocketTerminalHandler(c *gin.Context) {
	// 检查是否指定了代理UUID
	agentUUID := c.Query("agent")
	
	// 如果未指定代理或指定的是本地代理，直接使用WebSocket
	if agentUUID == "" || agentUUID == config.GetAppConfig().UUID {
		handleLocalWebSocketTerminal(c)
		return
	}
	
	// 否则，检查远程代理是否可用于WebSocket直连
	if isAgentDirectlyAccessible(agentUUID) {
		// 这里可以实现代理转发，但简单起见，我们先返回错误
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "Direct WebSocket connection to remote agents not implemented yet",
			"message": "Please use MQTT mode for remote agents",
		})
		return
	} else {
		// 如果远程代理不可直接访问，返回错误
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Remote agent not directly accessible",
			"message": "Please use MQTT mode for this agent",
		})
		return
	}
}

// 判断代理是否可直接访问（可以通过WebSocket直连）
func isAgentDirectlyAccessible(agentUUID string) bool {
	// 实际实现中，这里应该有检查代理是否可直接访问的逻辑
	// 比如尝试ping或其他连通性测试
	// 简单起见，我们先返回false，表示所有远程代理都不可直接访问
	return false
}

// 处理本地WebSocket终端连接
func handleLocalWebSocketTerminal(c *gin.Context) {
	log.Printf("[WS Terminal] Upgrading connection to WebSocket...")
	
	// Upgrade HTTP connection to WebSocket
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS Terminal] Failed to upgrade connection: %v", err)
		// 尝试发送错误响应给客户端
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to upgrade connection to WebSocket",
			"details": err.Error(),
		})
		return
	}
	
	log.Printf("[WS Terminal] WebSocket connection established, getting terminal manager...")

	// Get global terminal manager
	manager := wsterminal.GetGlobalManager()
	if manager == nil {
		log.Printf("[WS Terminal] Error: Terminal manager is nil")
		conn.WriteMessage(websocket.TextMessage, []byte("Internal error: Terminal manager not initialized"))
		conn.Close()
		return
	}

	log.Printf("[WS Terminal] Creating terminal session...")
	
	// Create terminal session
	terminal, err := manager.CreateTerminal(conn, "")
	if err != nil {
		log.Printf("[WS Terminal] Failed to create terminal: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to create terminal: "+err.Error()))
		conn.Close()
		return
	}

	log.Printf("[WS Terminal] Terminal created successfully, starting I/O...")
	
	// Start terminal I/O
	terminal.Start()
	
	log.Printf("[WS Terminal] Terminal I/O started")
}

// TerminalPageHandler serves the terminal HTML page with proper mode detection
func TerminalPageHandler(c *gin.Context) {
	// 检查是否指定了代理UUID
	agentUUID := c.Query("agent")
	
	// 确定终端模式
	mode := "ws" // 默认使用WebSocket模式
	
	// 如果指定了非本地代理，检查是否可以直接访问
	if agentUUID != "" && agentUUID != config.GetAppConfig().UUID {
		if !isAgentDirectlyAccessible(agentUUID) {
			mode = "mqtt" // 如果不可直接访问，使用MQTT模式
		}
	}
	
	c.HTML(http.StatusOK, "terminal.html", gin.H{
		"title": "Terminal",
		"mode": mode,
		"agentUUID": agentUUID,
	})
}

// WebSocketTerminalInfo 返回终端连接信息
func WebSocketTerminalInfo(c *gin.Context) {
	// 获取请求的代理UUID
	agentUUID := c.Query("agent")
	if agentUUID == "" {
		// 如果未指定代理，使用本地代理
		agentUUID = config.GetAppConfig().UUID
	}
	
	// 检查代理连接模式
	var mode string
	if agentUUID == config.GetAppConfig().UUID || isAgentDirectlyAccessible(agentUUID) {
		mode = "ws"
	} else {
		mode = "mqtt"
	}
	
	c.JSON(http.StatusOK, gin.H{
		"agentUUID": agentUUID,
		"mode": mode,
		"available": true, // 这里简化处理，假定代理总是可用的
	})
}