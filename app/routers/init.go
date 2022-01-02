package routers

import (
	"github.com/gin-gonic/gin"
	"proxy-manager/app/middlewares"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	engine.Use(middlewares.ErrorHttp)
	faviconRouter(engine)
	indexRouter(engine)
}
