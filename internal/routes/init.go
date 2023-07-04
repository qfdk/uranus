package routes

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"uranus/internal/config"
	"uranus/internal/controllers"
)

func auth(context *gin.Context) {
	var isAuth = false
	session := sessions.Default(context)
	isAuth = session.Get("login") == true

	if !isAuth {
		queryUrl := strings.Split(fmt.Sprint(context.Request.URL.String()), "?")[0]
		// 针对远程路由鉴权
		if queryUrl == "/admin/xterm.js" {
			if context.Query("token") == config.GetAppConfig().Token {
				isAuth = true
			}
		}
	}

	if isAuth {
		context.Next()
	} else {
		context.Redirect(http.StatusFound, "/")
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
