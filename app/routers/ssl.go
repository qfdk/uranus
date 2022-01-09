package routers

import (
	"github.com/gin-gonic/gin"
	"proxy-manager/app/controllers"
)

func sslRouter(engine *gin.Engine) {
	engine.GET("/ssl", controllers.GetCertificate)
}
