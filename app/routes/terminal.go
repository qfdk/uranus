package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/app/controllers"
	"uranus/app/pkg/xtermjs"
)

func terminalRoute(engine *gin.RouterGroup) {
	engine.GET("/terminal", controllers.Terminal)
	// this is the endpoint for xterm.js to connect to
	xtermjsHandlerOptions := xtermjs.HandlerOpts{
		Command:              "/bin/bash",
		ConnectionErrorLimit: 10,
		KeepalivePingTimeout: 20,
		MaxBufferSizeBytes:   512,
	}
	engine.GET("/xterm.js", xtermjs.GetHandler(xtermjsHandlerOptions))
}
