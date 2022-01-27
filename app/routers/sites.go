package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/controllers"
)

func sitesRouter(engine *gin.Engine) {
	engine.GET("/sites", controllers.GetSites)
	engine.GET("/sites/new", controllers.NewSite)
	engine.GET("/sites/template", controllers.GetTemplate)
	engine.GET("/sites/edit/:id", controllers.EditSiteConf)
	engine.GET("/sites/delete/:id", controllers.DeleteSiteConf)
	engine.POST("/sites/save", controllers.SaveSiteConf)
}
