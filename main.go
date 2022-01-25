package main

import (
	"embed"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/routers"
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
	//config.InitRedis()
	go tools.RenewSSL()
	r := gin.Default()
	t, _ := template.ParseFS(templates, "views/*")
	r.SetHTMLTemplate(t)
	// 静态文件路由
	r.StaticFS("/public", mustFS())
	r.SetTrustedProxies([]string{"127.0.0.1"})
	routers.RegisterRoutes(r)
	println("网站路径：" + config.GetAppConfig().VhostPath)
	r.Run(":7777")
}
