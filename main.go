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
	config.InitRedis()
	go tools.RenewSSL()
	app := gin.Default()
	template, _ := template.ParseFS(templates, "views/*")
	app.SetHTMLTemplate(template)

	// 静态文件路由
	app.StaticFS("/public", mustFS())
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := staticFS.ReadFile("public/icon/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", file)
	})

	app.SetTrustedProxies([]string{"127.0.0.1"})
	routes.RegisterRoutes(app)
	app.Run(":7777")
}
