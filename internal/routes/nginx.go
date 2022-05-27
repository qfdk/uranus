package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func nginxRoute(engine *gin.RouterGroup) {
	engine.POST("/nginx", controllers.Nginx)
	engine.POST("/nginx/save", controllers.SaveNginxConf)
	engine.GET("/nginx/config", controllers.GetNginxConf)
	engine.GET("/nginx/config-info", controllers.GetNginxCompileInfo)
}
