package routes

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"github.com/qfdk/nginx-proxy-manager/version"
	"github.com/spf13/viper"
	"runtime"
)

func publicRoute(engine *gin.Engine) {

	engine.GET("/info", func(context *gin.Context) {
		context.JSON(200, gin.H{
			"BuildName":    version.BuildName,
			"BuildTime":    version.BuildTime,
			"BuildVersion": version.BuildVersion,
			"GitCommit":    version.CommitID,
			"GoVersion":    runtime.Version(),
			"OS":           runtime.GOOS,
		})
	})

	engine.GET("/upgrade", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/nginx-proxy-manager")
		context.JSON(200, gin.H{
			"status":       "OK",
			"BuildTime":    version.BuildTime,
			"BuildVersion": version.BuildVersion})
	})

	engine.POST("/update-config", func(context *gin.Context) {
		data := gin.H{}
		rawData, err := context.GetRawData()
		if err != nil {
			panic(err)
		}
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		json.Unmarshal(rawData, &data)
		for key, value := range data {
			viper.Set(key, value)
		}
		viper.WriteConfig()
		context.JSON(200, gin.H{"status": "OK"})
	})
}
