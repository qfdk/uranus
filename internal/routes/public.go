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

// 版本信息响应结构体
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

	// 检查更新接口
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

	// 升级接口：支持本地调用和远程调用
	engine.POST("/upgrade", func(context *gin.Context) {
		// 检查是否是内部直接调用的场景（没有请求体或请求体为空）
		if context.Request.ContentLength == 0 {
			log.Printf("[升级] 本地直接调用升级接口")

			// 执行升级操作，无需验证token
			go func() {
				upgradeErr := services.ToUpdateProgram("https://fr.qfdk.me/uranus/uranus-" + runtime.GOARCH)
				if upgradeErr != nil {
					log.Printf("[升级] 升级过程出错: %v", upgradeErr)
				}
			}()

			context.JSON(200, gin.H{
				"status":  "OK",
				"message": "本地升级请求已接收，正在处理中",
			})
			return
		}

		// 远程调用场景，需要验证token
		var requestData map[string]interface{}
		if err := context.BindJSON(&requestData); err == nil {
			// 检查token是否匹配
			if token, exists := requestData["token"].(string); exists {
				// 验证token是否正确
				if token == config.GetAppConfig().Token {
					// 记录日志
					log.Printf("[升级] 收到远程升级请求，Token验证通过")

					// Token验证通过，执行升级操作
					go func() {
						// 使用协程执行升级，避免阻塞HTTP响应
						upgradeErr := services.ToUpdateProgram("https://fr.qfdk.me/uranus/uranus-" + runtime.GOARCH)
						if upgradeErr != nil {
							log.Printf("[升级] 升级过程出错: %v", upgradeErr)
						}
					}()

					// 立即返回成功响应
					context.JSON(200, gin.H{
						"status":  "OK",
						"message": "远程升级请求已接收，正在处理中",
					})
					return
				} else {
					// Token不匹配
					log.Printf("[升级] 远程升级请求Token验证失败")
					context.JSON(401, gin.H{
						"status":  "error",
						"message": "无效的Token",
					})
					return
				}
			}
		}

		// 如果没有提供Token或解析失败，返回错误
		log.Printf("[升级] 远程请求格式无效或缺少Token")
		context.JSON(400, gin.H{
			"status":  "error",
			"message": "请求无效，缺少有效的Token",
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
