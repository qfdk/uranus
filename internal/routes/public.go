package routes

import (
	"encoding/json"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"io/ioutil"
	"net/http"
	"runtime"
	"uranus/internal/config"
	"uranus/internal/services"
)

type VersionResponse struct {
	ID           string `json:"_id"`
	CommitID     string `json:"commitId"`
	V            int    `json:"__v"`
	BuildTime    string `json:"buildTime"`
	BuildVersion string `json:"buildVersion"`
}

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
		if username == config.GetAppConfig().Username && password == config.GetAppConfig().Password {
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
			"buildName":    config.BuildName,
			"buildTime":    config.BuildTime,
			"buildVersion": config.BuildVersion,
			"commitId":     config.CommitID,
			"goVersion":    runtime.Version(),
			"os":           runtime.GOOS,
			"uid":          config.GetAppConfig().Uid,
		})
	})

	engine.GET("/upgrade", func(context *gin.Context) {
		fmt.Println("升级请求 /upgrade")
		resp, err := http.Get("https://misaka.qfdk.me/version")
		if err != nil {
			// handle err
			fmt.Println(err)
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		var response VersionResponse
		json.Unmarshal(body, &response)
		fmt.Println("response=====")
		fmt.Println(response)
		fmt.Println("response end=====")

		if config.CommitID != response.CommitID {
			services.ToUpdateProgram("https://fr.qfdk.me/uranus")
			context.JSON(200, gin.H{
				"status":       "OK",
				"buildTime":    response.BuildTime,
				"commitId":     response.CommitID,
				"buildVersion": response.BuildVersion,
			})
		} else {
			context.JSON(200, gin.H{
				"status":       "KO",
				"buildTime":    config.BuildTime,
				"commitId":     config.CommitID,
				"buildVersion": config.BuildVersion,
			})
		}
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
