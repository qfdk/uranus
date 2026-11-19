package routes

import (
	"crypto/subtle"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"runtime"
	"uranus/internal/config"
	"uranus/internal/services"
)

// 升级相关操作，测试中替换为桩函数，避免真的下载和替换程序
var (
	checkSelfUpdate = services.CheckSelfUpdate
	runUpgrade      = services.ToUpdateProgram
)

// secureEqual 以常量时间比较凭证；期望值为空时一律不通过
func secureEqual(given, expected string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(given), []byte(expected)) == 1
}

func publicRoute(engine *gin.Engine) {

	// 登录路由
	engine.GET("/", func(context *gin.Context) {
		session := sessions.Default(context)
		if session.Get("login") == true {
			context.Redirect(http.StatusFound, "/admin/dashboard")
			context.Abort()
		} else {
			context.HTML(http.StatusOK, "login.html", gin.H{
				"loginError": context.Query("error") != "",
			})
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
		appConfig := config.GetAppConfig()
		if secureEqual(username, appConfig.Username) && secureEqual(password, appConfig.Password) {
			session.Set("login", true)
			_ = session.Save()
			context.Redirect(http.StatusFound, "/admin/dashboard")
		} else {
			context.Redirect(http.StatusFound, "/?error=1")
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

	// 检查更新接口：发布清单要有正确的签名，按发布编号判断新旧
	engine.GET("/checkUpdate", func(context *gin.Context) {
		manifest, err := services.FetchManifest()
		if err != nil {
			log.Printf("[-] 检查更新出错: %v", err)
			context.JSON(200, gin.H{
				"status":       "KO",
				"buildTime":    "检查更新出错",
				"commitId":     "检查更新出错",
				"buildVersion": "检查更新出错",
			})
			return
		}
		if manifest.Newer() {
			context.JSON(200, gin.H{
				"status":       "OK",
				"buildTime":    manifest.BuildTime,
				"commitId":     manifest.CommitID,
				"buildVersion": manifest.BuildVersion,
			})
			return
		}
		context.JSON(200, gin.H{
			"status":       "KO",
			"buildTime":    config.BuildTime,
			"commitId":     config.CommitID,
			"buildVersion": config.BuildVersion,
		})
	})

	// 升级接口：仪表盘（已登录会话）或携带 token 的远程调用
	engine.POST("/upgrade", func(context *gin.Context) {
		if !isLoggedIn(context) {
			var request struct {
				Token string `json:"token"`
			}
			if err := context.ShouldBindJSON(&request); err != nil || !secureEqual(request.Token, config.GetAppConfig().Token) {
				log.Printf("[升级] 拒绝未授权的升级请求，来源: %s", context.ClientIP())
				context.JSON(http.StatusUnauthorized, gin.H{
					"status":  "error",
					"message": "未登录或 token 无效",
				})
				return
			}
			log.Printf("[升级] 收到远程升级请求，Token验证通过")
		}

		if err := checkSelfUpdate(); err != nil {
			context.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		// 使用协程执行升级，避免阻塞HTTP响应
		go func() {
			if err := runUpgrade(); err != nil {
				log.Printf("[升级] 升级过程出错: %v", err)
			}
		}()

		context.JSON(http.StatusOK, gin.H{
			"status":  "OK",
			"message": "升级请求已接收，正在处理中",
		})
	})

	// 控制中心下发配置：必须同时带上本机 uuid 和 token。uuid 会通过 /info 公开，不能单独作为凭证。
	engine.POST("/update-config", func(context *gin.Context) {
		data := map[string]interface{}{}
		if err := context.ShouldBindJSON(&data); err != nil {
			context.JSON(http.StatusBadRequest, gin.H{"status": "KO", "message": "请求格式无效"})
			return
		}

		appConfig := config.GetAppConfig()
		uuid, _ := data["uuid"].(string)
		token, _ := data["token"].(string)
		if uuid != appConfig.UUID || !secureEqual(token, appConfig.Token) {
			log.Printf("[CONFIG] 拒绝未授权的配置更新请求，来源: %s", context.ClientIP())
			context.JSON(http.StatusUnauthorized, gin.H{"status": "KO"})
			return
		}

		// uuid 和 token 只用于鉴权，不作为要更新的配置
		configData := make(map[string]interface{})
		for key, value := range data {
			if key != "uuid" && key != "token" {
				configData[key] = value
			}
		}

		if len(configData) > 0 {
			updatedKeys, err := services.UpdateAgentConfig(configData)
			if err != nil {
				log.Printf("[CONFIG] HTTP配置更新失败: %v", err)
				context.JSON(500, gin.H{"status": "ERROR", "message": err.Error()})
				return
			}
			log.Printf("[CONFIG] HTTP配置更新成功，更新的字段: %v", updatedKeys)
		}

		context.JSON(200, gin.H{"status": "OK"})
	})
}
