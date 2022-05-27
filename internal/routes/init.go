package routes

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"uranus/internal/controllers"
)

func auth(context *gin.Context) {
	session := sessions.Default(context)
	if session.Get("login") == true {
		context.Next()
	} else {
		context.Redirect(http.StatusMovedPermanently, "/")
	}
	context.Abort()
}

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 初始化路由
	//websocketRoute(engine)
	engine.Use(sessions.Sessions("uranus", cookie.NewStore([]byte("secret"))))
	publicRoute(engine)
	authorized := engine.Group("/admin", auth)
	authorized.GET("/dashboard", controllers.Index)
	nginxRoute(authorized)
	sitesRoute(authorized)
	sslRoute(authorized)
	terminalRoute(authorized)
}