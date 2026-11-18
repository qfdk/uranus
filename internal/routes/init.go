package routes

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"uranus/internal/config"
	"uranus/internal/controllers"
)

// sessionMaxAge 登录状态的有效期（秒）
const sessionMaxAge = 30 * 24 * 3600

func auth(context *gin.Context) {
	if !isLoggedIn(context) {
		context.Redirect(http.StatusFound, "/")
		context.Abort()
		return
	}
	context.Next()
}

func isLoggedIn(context *gin.Context) bool {
	return sessions.Default(context).Get("login") == true
}

// RegisterRoutes /** 路由组*/
func RegisterRoutes(engine *gin.Engine) {
	// 错误中间件
	//engine.Use(middlewares.ErrorHttp)
	// 初始化路由
	// 会话 cookie 用本机随机生成的密钥签名。以前用固定的 "secret"，任何人都能伪造登录状态。
	store := cookie.NewStore([]byte(config.GetAppConfig().SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		// 跨站发起的 POST 不带 cookie，防止其他网站借管理员的登录状态提交表单
		SameSite: http.SameSiteLaxMode,
	})
	engine.Use(sessions.Sessions("uranus", store))
	publicRoute(engine)
	authorized := engine.Group("/admin", auth)
	authorized.GET("/dashboard", controllers.Index)
	nginxRoute(authorized)
	sitesRoute(authorized)
	sslRoute(authorized)
	terminalRoute(authorized)
	configRoute(authorized)
}
