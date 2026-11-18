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
	// 删除用 POST：会话 cookie 是 SameSite=Lax，别的网站发起的跳转（GET）仍会带上它
	engine.POST("/sites/delete/:filename", controllers.DeleteSiteConf)
	engine.POST("/sites/save", controllers.SaveSiteConf)
}
