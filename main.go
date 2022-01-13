package main

import (
	"github.com/foolin/goview"
	"github.com/foolin/goview/supports/ginview"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/routers"
	"github.com/qfdk/nginx-proxy-manager/app/tools"
	"github.com/qfdk/nginx-proxy-manager/config"
)

func main() {
	go tools.RenewSSL()
	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	println("网站路径：" + config.GetAppConfig().VhostPath)
	//new template engine
	r.HTMLRender = ginview.New(goview.Config{
		Root:         "web",
		Extension:    ".html",
		Master:       "layouts/master",
		Partials:     []string{},
		DisableCache: false,
	})
	routers.RegisterRoutes(r)
	r.Run(":7777")
}
