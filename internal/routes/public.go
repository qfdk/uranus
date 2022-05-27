package routes

import (
	"encoding/json"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"net/http"
	"runtime"
	config2 "uranus/internal/config"
	"uranus/internal/services"
)

func publicRoute(engine *gin.Engine) {

	// 登录路由
	engine.GET("/", func(context *gin.Context) {
		session := sessions.Default(context)
		if session.Get("login") == true {
			context.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
			context.Abort()
		} else {
			context.HTML(200, "login.html", gin.H{})
		}
	})

	engine.GET("/logout", func(context *gin.Context) {
		session := sessions.Default(context)
		if session.Get("login") == true {
			session.Delete("login")
			session.Save()
		}
		context.Redirect(http.StatusMovedPermanently, "/")
	})

	engine.POST("/login", func(context *gin.Context) {
		session := sessions.Default(context)
		username, _ := context.GetPostForm("username")
		password, _ := context.GetPostForm("password")
		if username == config2.GetAppConfig().Username && password == config2.GetAppConfig().Password {
			session.Set("login", true)
			session.Save()
			context.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
		} else {
			context.Redirect(http.StatusMovedPermanently, "/")
		}
		context.Abort()
	})

	engine.GET("/info", func(context *gin.Context) {
		context.JSON(200, gin.H{
			"buildName":    config2.BuildName,
			"buildTime":    config2.BuildTime,
			"buildVersion": config2.BuildVersion,
			"gitCommit":    config2.CommitID,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"uid":          config2.GetAppConfig().Uid,
		})
	})

	engine.GET("/upgrade", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/uranus")
		context.JSON(200, gin.H{
			"status":       "OK",
			"buildTime":    config2.BuildTime,
			"gitCommit":    config2.CommitID,
			"buildVersion": config2.BuildVersion})
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
