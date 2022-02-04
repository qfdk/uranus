package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
)

func nginxRoute(engine *gin.Engine) {
	engine.POST("/nginx", controllers.Nginx)
	engine.POST("/nginx/save", controllers.SaveNginxConf)
	engine.GET("/nginx/config", controllers.GetNginxConf)
	engine.GET("/nginx/compile-info", controllers.GetNginxCompileInfo)
}
