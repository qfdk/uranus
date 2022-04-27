package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/app/controllers"
)

func sslRoute(engine *gin.RouterGroup) {
	engine.GET("/ssl", controllers.Certificates)
	engine.GET("/ssl/renew", controllers.IssueCert)
	engine.GET("/ssl/info", controllers.CertInfo)
	engine.GET("/ssl/delete", controllers.DeleteSSL)
}
