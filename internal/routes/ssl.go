package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func sslRoute(engine *gin.RouterGroup) {
	engine.GET("/ssl", controllers.Certificates)
	engine.GET("/ssl/info", controllers.CertInfo)
	// 续签和删除会改变证书，用 POST：会话 cookie 是 SameSite=Lax，别的网站发起的跳转（GET）仍会带上它
	engine.POST("/ssl/renew", controllers.IssueCert)
	engine.POST("/ssl/delete", controllers.DeleteSSL)
}
