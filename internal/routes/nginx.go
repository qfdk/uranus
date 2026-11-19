package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func nginxRoute(engine *gin.RouterGroup) {
	engine.POST("/nginx", controllers.Nginx)
	engine.POST("/nginx/save", controllers.SaveNginxConf)
	engine.GET("/nginx/config", controllers.GetNginxConf)
	engine.GET("/nginx/config-info", controllers.GetNginxCompileInfo)
	// 配置文件保存之前的版本
	engine.GET("/nginx/history", controllers.ConfigHistory)
	engine.GET("/nginx/history/:id", controllers.ConfigVersionContent)
}
