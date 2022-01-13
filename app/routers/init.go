package routers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"github.com/qfdk/nginx-proxy-manager/config"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	engine.Use(gin.BasicAuth(gin.Accounts{config.GetAppConfig().Username: config.GetAppConfig().Password}))
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 静态文件路由
	engine.StaticFS("/public", http.Dir("./web/public"))
	// 初始化路由
	indexRouter(engine)
	sitesRouter(engine)
	sslRouter(engine)
}
