package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/app/controllers"
)

func nginxRoute(engine *gin.Engine) {
	engine.POST("/nginx", controllers.Nginx)
	engine.POST("/nginx/save", controllers.SaveNginxConf)
	engine.GET("/nginx/config", controllers.GetNginxConf)
	engine.GET("/nginx/config-info", controllers.GetNginxCompileInfo)
}
