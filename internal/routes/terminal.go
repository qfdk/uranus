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
}
