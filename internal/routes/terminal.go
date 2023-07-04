package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func terminalRoute(engine *gin.RouterGroup) {
	engine.GET("/terminal", controllers.TerminalStart)
	engine.GET("/terminal/stop", controllers.TerminalStop)
}
