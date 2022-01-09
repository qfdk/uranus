package routers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 静态文件路由
	engine.StaticFS("/public", http.Dir("./views/public"))
	// 初始化路由
	indexRouter(engine)
	sitesRouter(engine)
	sslRouter(engine)
}
