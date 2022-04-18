package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/app/controllers"
)

func websocketRoute(engine *gin.Engine) {
	engine.GET("/ws-status", controllers.Websocket)
}
