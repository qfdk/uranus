package routes

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"runtime"
	"syscall"
	"uranus/app/config"
	"uranus/app/services"
)

func publicRoute(engine *gin.Engine) {

	engine.GET("/info", func(context *gin.Context) {
		context.JSON(200, gin.H{
			"buildName":    config.BuildName,
			"buildTime":    config.BuildTime,
			"buildVersion": config.BuildVersion,
			"gitCommit":    config.CommitID,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"uid":          config.GetAppConfig().Uid,
		})
	})

	engine.GET("/upgrade", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/uranus")
		context.JSON(200, gin.H{
			"status":       "OK",
			"buildTime":    config.BuildTime,
			"gitCommit":    config.CommitID,
			"buildVersion": config.BuildVersion})
	})
	engine.GET("/restart", func(context *gin.Context) {
		syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
		context.JSON(200, gin.H{"status": "OK"})
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
