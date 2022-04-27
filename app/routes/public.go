package routes

import (
	"encoding/json"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"net/http"
	"runtime"
	"uranus/app/config"
	"uranus/app/services"
)

func publicRoute(engine *gin.Engine) {

	// 登录路由
	engine.GET("/", func(context *gin.Context) {
		session := sessions.Default(context)
		if session.Get("login") == true {
			fmt.Println("已经登录了")
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
		if username == config.GetAppConfig().Username && password == config.GetAppConfig().Password {
			session.Set("login", true)
			session.Save()
			fmt.Println("登录成功")
			context.Redirect(http.StatusMovedPermanently, "/admin/dashboard")
		} else {
			fmt.Println("登录失败")
			context.Redirect(http.StatusMovedPermanently, "/")
		}
		context.Abort()
	})

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
