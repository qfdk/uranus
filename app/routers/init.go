package routers

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes /** 路由组*/
func RegisterRoutes(router *gin.Engine) {
	indexRouter(router)
}
