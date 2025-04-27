package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func terminalRoute(engine *gin.RouterGroup) {
	engine.GET("/terminal", controllers.TerminalStart)
	engine.GET("/terminal/stop", controllers.TerminalStop)

	// 添加MQTT终端路由
	engine.GET("/terminal/mqtt", controllers.MQTTTerminalStart)
	engine.GET("/terminal/mqtt/list", controllers.MQTTTerminalList)
	engine.GET("/terminal/mqtt/:sessionID/close", controllers.MQTTTerminalClose)
}
