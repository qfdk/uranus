package routes

import (
	"github.com/gin-gonic/gin"
	"uranus/internal/controllers"
)

func configRoute(engine *gin.RouterGroup) {
	engine.GET("/config/edit", controllers.GetConfigEditor)
	engine.POST("/config/save", controllers.SaveConfig)
}
