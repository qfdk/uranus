package main

import (
	"embed"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/routes"
	"github.com/qfdk/nginx-proxy-manager/app/tools"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
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

func headersByRequestURI() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/public/icon") {
			c.Header("Cache-Control", "max-age=86400")
			c.Header("Content-Description", "File Transfer")
			c.Header("Content-Type", "application/octet-stream")
			c.Header("Content-Transfer-Encoding", "binary")
		}
		//else if strings.HasPrefix(c.Request.RequestURI, "/icon/") {
		//	c.Header("Cache-Control", "max-age=86400")
		//}
	}
}

func main() {
	// 线上模式显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		displayVersion()
	}

	app := gin.New()
	template, _ := template.ParseFS(templates, "views/*")
	app.SetHTMLTemplate(template)

	// 静态文件路由
	app.Use(headersByRequestURI())
	app.StaticFS("/public", mustFS())
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := staticFS.ReadFile("public/icon/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", file)
	})
	app.SetTrustedProxies([]string{"127.0.0.1"})
	routes.RegisterRoutes(app)
	config.InitRedis()
	go tools.RenewSSL()
	app.Run(":7777")
}
