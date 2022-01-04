package routers

import (
	"github.com/gin-gonic/gin"
	"proxy-manager/app/controllers"
)

func sitesRouter(engine *gin.Engine) {
	engine.GET("/sites", controllers.GetSites)
	engine.GET("/sites/edit", controllers.EditSiteConf)
	engine.GET("/sites/delete", controllers.DeleteSiteConf)
	//engine.GET("/sites/:name", controllers.EditSiteConf)
}
