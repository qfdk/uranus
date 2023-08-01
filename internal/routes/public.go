package routes

import (
	"encoding/json"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"io"
	"log"
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
			context.Redirect(http.StatusFound, "/admin/dashboard")
			context.Abort()
		} else {
			context.HTML(http.StatusOK, "login.html", gin.H{})
		}
	})

	engine.GET("/logout", func(context *gin.Context) {
		session := sessions.Default(context)
		if session.Get("login") == true {
			session.Delete("login")
			_ = session.Save()
		}
		context.Redirect(http.StatusFound, "/")
	})

	engine.POST("/login", func(context *gin.Context) {
		session := sessions.Default(context)
		username, _ := context.GetPostForm("username")
		password, _ := context.GetPostForm("password")
		if username == config.GetAppConfig().Username && password == config.GetAppConfig().Password {
			session.Set("login", true)
			_ = session.Save()
			context.Redirect(http.StatusFound, "/admin/dashboard")
		} else {
			context.Redirect(http.StatusFound, "/")
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
			"uuid":         config.GetAppConfig().UUID,
		})
	})

	engine.GET("/checkUpdate", func(context *gin.Context) {
		resp, err := http.Get("https://fr.qfdk.me/uranus.php")
		if err != nil {
			log.Println("[-] 检查更新出错")
			log.Println(err)
			context.JSON(200, gin.H{
				"status":       "KO",
				"buildTime":    "检查更新出错",
				"commitId":     "检查更新出错",
				"buildVersion": "检查更新出错",
			})
			return
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)
		body, _ := io.ReadAll(resp.Body)
		var response VersionResponse
		if json.Unmarshal(body, &response) == nil {
			if config.CommitID != response.CommitID {
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
		} else {
			context.JSON(200, gin.H{
				"status":       "KO",
				"buildTime":    "解析版本信息出错",
				"commitId":     "解析版本信息出错",
				"buildVersion": "解析版本信息出错",
			})
		}
	})

	engine.POST("/upgrade", func(context *gin.Context) {
		services.ToUpdateProgram("https://fr.qfdk.me/uranus/uranus-" + runtime.GOARCH)
		context.JSON(200, gin.H{
			"status": "OK",
		})
	})

	engine.POST("/update-config", func(context *gin.Context) {
		// 是否验证下token
		data := gin.H{}
		rawData, err := context.GetRawData()
		if err != nil {
			panic(err)
		}
		_ = json.Unmarshal(rawData, &data)
		if data["uuid"] == config.GetAppConfig().UUID {
			viper.SetConfigName("config")
			viper.SetConfigType("toml")
			viper.AddConfigPath(".")
			for key, value := range data {
				viper.Set(key, value)
			}
			_ = viper.WriteConfig()
			context.JSON(200, gin.H{"status": "OK"})
		} else {
			context.JSON(200, gin.H{"status": "KO"})
		}
	})
}
