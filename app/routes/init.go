package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 初始化路由
	websocketRoute(engine)
	engine.GET("/api/info", func(context *gin.Context) {
		config := config.GetAppConfig()
		context.JSON(200, gin.H{"url": config.Url, "id": config.Id})
	})
	engine.Use(gin.BasicAuth(gin.Accounts{config.GetAppConfig().Username: config.GetAppConfig().Password}))
	engine.GET("/", controllers.Index)
	nginxRoute(engine)
	sitesRoute(engine)
	sslRoute(engine)
}
