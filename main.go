package main

import (
	"github.com/foolin/goview"
	"github.com/foolin/goview/supports/ginview"
	"github.com/gin-gonic/gin"
	"proxy-manager/app/routers"
	"proxy-manager/app/tools"
	"proxy-manager/config"
)

func main() {
	go tools.RenewSSL()
	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	println("Nginx vhost 路径：" + config.GetAppConfig().VhostPath)
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
