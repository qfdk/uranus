package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/app/controllers"
)

func sitesRoute(engine *gin.Engine) {
	engine.GET("/sites", controllers.GetSites)
	engine.GET("/sites/new", controllers.NewSite)
	engine.GET("/sites/template", controllers.GetTemplate)
	engine.GET("/sites/edit/:filename", controllers.EditSiteConf)
	engine.GET("/sites/delete/:filename", controllers.DeleteSiteConf)
	engine.POST("/sites/save", controllers.SaveSiteConf)
}
