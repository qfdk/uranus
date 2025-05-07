package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
	"uranus/internal/wsterminal"
)

func terminalRoute(engine *gin.RouterGroup) {
	// 初始化WebSocket终端管理器
	wsterminal.InitGlobalManager()
	
	// 终端页面路由
	engine.GET("/terminal", controllers.TerminalPageHandler)
	
	// WebSocket终端路由
	engine.GET("/ws/terminal", controllers.WebSocketTerminalHandler)
	
	// 终端API路由
	terminalAPI := engine.Group("/api/terminal")
	{
		// 终端连接信息API
		terminalAPI.GET("/info", controllers.WebSocketTerminalInfo)
		
		// MQTT终端API
		terminalAPI.GET("/mqtt/connect", controllers.MQTTTerminalConnect)
		terminalAPI.POST("/mqtt/command", controllers.SendMQTTTerminalCommand)
	}
	
	// 保留旧的基于ttyd的终端路由以向后兼容（如果需要）
	// 这些路由在未来版本中可能会被移除
	engine.GET("/terminal/start", controllers.TerminalStart)
	engine.GET("/terminal/stop", controllers.TerminalStop)
}
