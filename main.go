package main

import (
	"embed"
	"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/middlewares"
	"github.com/qfdk/nginx-proxy-manager/app/routes"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"github.com/qfdk/nginx-proxy-manager/version"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"syscall"
)

//go:embed views
var templates embed.FS

//go:embed public
var staticFS embed.FS

func mustFS() http.FileSystem {
	sub, err := fs.Sub(staticFS, "public")

	if err != nil {
		panic(err)
	}

	return http.FS(sub)
}
func main() {
	// 线上模式显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		version.DisplayVersion()
	}
	// 初始化配置文件
	config.InitAppConfig()
	// 初始化redis
	config.InitRedis()
	defer config.CloseRedis()

	app := gin.New()
	template, _ := template.ParseFS(templates, "views/includes/*.html", "views/*.html")
	app.SetHTMLTemplate(template)
	// 缓存中间件
	app.Use(middlewares.CacheMiddleware())
	// 静态文件路由
	app.StaticFS("/public", mustFS())
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := staticFS.ReadFile("public/icon/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", file)
	})
	app.SetTrustedProxies([]string{"127.0.0.1"})
	routes.RegisterRoutes(app)
	go services.RenewSSL()
	server := endless.NewServer("0.0.0.0:7777", app)
	server.BeforeBegin = func(add string) {
		log.Printf("[+] 服务器启动, PID: %d", syscall.Getpid())
	}
	server.ListenAndServe()
}
