package main

import (
	"fmt"
	"github.com/foolin/goview"
	"github.com/foolin/goview/supports/ginview"
	"github.com/gin-gonic/gin"
	"proxy-manager/app/routers"
	"proxy-manager/config"
)

func main() {
	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	fmt.Println(config.GetAppConfig().VhostPath)
	//new template engine
	r.HTMLRender = ginview.New(goview.Config{
		Root:         "views",
		Extension:    ".html",
		Master:       "layouts/master",
		Partials:     []string{},
		DisableCache: false,
	})
	routers.RegisterRoutes(r)
	r.Run(":7777")
}
