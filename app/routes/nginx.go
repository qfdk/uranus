package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
)

func nginxRoute(engine *gin.Engine) {
	engine.GET("/nginx", controllers.Nginx)
	engine.POST("/nginx", controllers.SaveNginxConf)
	engine.GET("/nginx/config", controllers.GetNginxCompileInfo)
}
