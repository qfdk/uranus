package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func sitesRoute(engine *gin.RouterGroup) {
	engine.GET("/sites", controllers.GetSites)
	engine.GET("/sites/new", controllers.NewSite)
	engine.GET("/sites/template", controllers.GetTemplate)
	engine.GET("/sites/edit/:filename", controllers.EditSiteConf)
	engine.GET("/sites/delete/:filename", controllers.DeleteSiteConf)
	engine.POST("/sites/save", controllers.SaveSiteConf)
}
