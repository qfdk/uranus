package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func nginxRoute(engine *gin.RouterGroup) {
	engine.POST("/nginx", controllers.Nginx)
	engine.GET("/nginx/config", controllers.GetNginxConf)
	// Nginx 配置目录里的文件
	engine.GET("/nginx/files", controllers.ConfFiles)
	engine.GET("/nginx/file", controllers.EditConfFile)
	engine.POST("/nginx/file/save", controllers.SaveConfFile)
	engine.POST("/nginx/test", controllers.CheckNginxConf)
	// Nginx 的日志
	engine.GET("/nginx/logs", controllers.NginxLogs)
	engine.GET("/nginx/logs/tail", controllers.TailNginxLog)
	engine.GET("/nginx/config-info", controllers.GetNginxCompileInfo)
	// 配置文件保存之前的版本
	engine.GET("/nginx/history", controllers.ConfigHistory)
	engine.GET("/nginx/history/:id", controllers.ConfigVersionContent)
}
