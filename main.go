package main

import (
	"github.com/gin-gonic/gin"
	"proxy-manager/app/routers"
	"github.com/foolin/goview/supports/ginview"
	"github.com/foolin/goview"
)

func main() {
	r := gin.Default()
	r.SetTrustedProxies([]string{"127.0.0.1"})
	//new template engine
	r.HTMLRender = ginview.New(goview.Config{
		Root:         "views",
		Extension:    ".html",
		Master:       "layouts/master",
		Partials:     []string{},
		DisableCache: false,
	})
	routers.RegisterRoutes(r)
	r.Run(":8080")
}
