package routes

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"github.com/qfdk/nginx-proxy-manager/version"
	"github.com/spf13/viper"
	"runtime"
	"syscall"
)

func publicRoute(engine *gin.Engine) {

	engine.GET("/info", func(context *gin.Context) {
		context.JSON(200, gin.H{
			"buildName":    version.BuildName,
			"buildTime":    version.BuildTime,
			"buildVersion": version.BuildVersion,
			"gitCommit":    version.CommitID,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"uid":          config.GetAppConfig().Uid,
		})
	})

	engine.GET("/upgrade", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/nginx-proxy-manager")
		context.JSON(200, gin.H{
			"status":       "OK",
			"buildTime":    version.BuildTime,
			"buildVersion": version.BuildVersion})
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
