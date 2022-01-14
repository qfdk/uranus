package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
	"github.com/qfdk/nginx-proxy-manager/config"
	"net/http"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	engine.Use(gin.BasicAuth(gin.Accounts{config.GetAppConfig().Username: config.GetAppConfig().Password}))
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 静态文件路由
	engine.StaticFS("/public", http.Dir("./web/public"))
	// 初始化路由
	engine.GET("/", controllers.Index)
	engine.GET("/config", controllers.GetNginxCompileInfo)
	engine.POST("/nginx", controllers.Nginx)
	engine.GET("/ws", ws)
	sitesRouter(engine)
	sslRouter(engine)
}
