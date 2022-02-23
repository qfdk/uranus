package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"time"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 初始化路由
	websocketRoute(engine)
	engine.GET("/info", func(context *gin.Context) {
		context.JSON(200, gin.H{"date": time.Now()})
	})
	engine.GET("/api/info", func(context *gin.Context) {
		config := config.GetAppConfig()
		context.JSON(200, gin.H{"key": config.Url, "uid": config.Id})
	})
	engine.GET("/update", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/nginx-proxy-manager")
		context.JSON(200, gin.H{"message": "更新成功"})
	})
	engine.Use(gin.BasicAuth(gin.Accounts{config.GetAppConfig().Username: config.GetAppConfig().Password}))
	engine.GET("/", controllers.Index)
	nginxRoute(engine)
	sitesRoute(engine)
	sslRoute(engine)
}
