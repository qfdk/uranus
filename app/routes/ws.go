package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
)

func websocketRoute(engine *gin.Engine) {
	engine.GET("/ws-status", controllers.Websocket)
}
