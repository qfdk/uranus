package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
	"uranus/pkg/xtermjs"
)

func terminalRoute(engine *gin.RouterGroup) {
	//engine.GET("/terminal", controllers.Terminal)
	engine.GET("/terminal", controllers.TerminalStart)
	engine.GET("/terminal/stop", controllers.TerminalStop)
	// this is the endpoint for xterm.js to connect to
	xtermjsHandlerOptions := xtermjs.HandlerOpts{
		Command:              "/bin/bash",
		ConnectionErrorLimit: 10,
		KeepalivePingTimeout: 20,
		MaxBufferSizeBytes:   512,
	}
	engine.GET("/xterm.js", xtermjs.GetHandler(xtermjsHandlerOptions))
}
