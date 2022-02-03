package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
	"github.com/qfdk/nginx-proxy-manager/app/middlewares"
)

func nginxRoute(engine *gin.Engine) {
	engine.POST("/nginx", controllers.Nginx)
	engine.POST("/nginx/save", controllers.SaveNginxConf)
	engine.GET("/nginx/config", middlewares.NoCache, controllers.GetNginxCompileInfo)
}
