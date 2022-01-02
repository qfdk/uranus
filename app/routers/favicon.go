package routers

import (
	"github.com/gin-gonic/gin"
)

// faviconRouter /** 定义index路由组
func faviconRouter(engine *gin.Engine) {
	engine.StaticFile("/favicon.ico", "./views/public/icon/favicon.ico")
	engine.StaticFile("/favicon-16x16.png", "./views/public/icon/favicon-16x16.png")
	engine.StaticFile("/favicon-32x32.png", "./views/public/icon/favicon-32x32.png")
}
