package routes

import (
	"github.com/gin-gonic/gin"
	"nginx-proxy-manager/app/controllers"
)

func sslRoute(engine *gin.Engine) {
	engine.GET("/ssl", controllers.Certificates)
	engine.GET("/ssl/renew", controllers.IssueCert)
	engine.GET("/ssl/info", controllers.CertInfo)
	engine.GET("/ssl/delete", controllers.DeleteSSL)
}
