package routers

import (
	"github.com/gin-gonic/gin"
	"proxy-manager/app/controllers"
)

// IndexRouter /** 定义index路由组
func indexRouter(engine *gin.Engine) {
	engine.GET("/", controllers.Index)
	engine.POST("/nginx", controllers.Nginx)
	engine.GET("/config", controllers.GetNginxCompileInfo)
	engine.GET("/domains", controllers.Domains)
	engine.GET("/ssl", controllers.SSLSettings)
}
