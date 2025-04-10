package main

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
	"time"
	"uranus/internal/config"
	"uranus/internal/middlewares"
	"uranus/internal/models"
	"uranus/internal/routes"
	"uranus/internal/services"
	"uranus/internal/tools"

	"github.com/cloudflare/tableflip"
	"github.com/gin-gonic/gin"
)

//go:embed web
var templates embed.FS

//go:embed web/public
var staticFS embed.FS

// 初始化路由器
func initRouter() *gin.Engine {
	app := gin.New()
	app.Use(gin.Recovery())
	app.Use(middlewares.CacheMiddleware())

	// 设置模板函数
	funcMap := template.FuncMap{
		"isEven": func(num int) bool {
			return num%2 == 0
		},
		"add": func(a, b int) int {
			return a + b
		},
		"svgIcon": func(name string) template.HTML {
			content, err := staticFS.ReadFile("web/public/icons/" + name + ".svg")
			if err != nil {
				return ""
			}
			return template.HTML(content)
		},
	}

	// 解析模板
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templates, "web/includes/*.html", "web/*.html")
	if err != nil {
		log.Fatalf("解析模板错误: %v", err)
	}
	app.SetHTMLTemplate(tmpl)

	// 静态文件处理
	subFS, err := fs.Sub(staticFS, "web/public")
	if err != nil {
		log.Fatalf("静态文件子系统错误: %v", err)
	}
	app.StaticFS("/public", http.FS(subFS))

	// favicon处理
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, err := staticFS.ReadFile("web/public/icon/favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", file)
	})

	// 设置受信任的代理
	if err := app.SetTrustedProxies([]string{"127.0.0.1"}); err != nil {
		log.Printf("设置受信任代理失败: %v", err)
	}

	// 注册路由
	routes.RegisterRoutes(app)

	return app
}

// 配置服务器
func configureServer(router *gin.Engine) *http.Server {
	return &http.Server{
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}
}

// 检查升级触发器
func checkUpgradeTrigger(upg *tableflip.Upgrader) {
	triggerPaths := []string{
		path.Join("/etc/uranus", ".upgrade_trigger"),
		path.Join(tools.GetPWD(), ".upgrade_trigger"),
	}

	// 删除启动时可能存在的触发文件
	for _, filePath := range triggerPaths {
		if _, err := os.Stat(filePath); err == nil {
			log.Printf("启动时发现触发文件 %s，正在删除", filePath)
			os.Remove(filePath)
		}
	}

	// 定期检查触发文件
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, triggerPath := range triggerPaths {
			if _, err := os.Stat(triggerPath); err == nil {
				log.Printf("检测到触发文件 %s，准备升级", triggerPath)
				os.Remove(triggerPath)
				services.DeleteAllBackups()

				if err := upg.Upgrade(); err != nil {
					log.Printf("升级失败: %v", err)
				}
				break
			}
		}
	}
}

func main() {
	// 初始化日志
	if gin.Mode() == gin.ReleaseMode {
		logDir := path.Join(tools.GetPWD(), "logs")
		if err := os.MkdirAll(logDir, 0755); err != nil && !os.IsExist(err) {
			log.Fatalf("无法创建日志目录: %v", err)
		}

		logFile, err := os.OpenFile(path.Join(logDir, "app.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("无法创建日志文件: %v", err)
		}
		defer logFile.Close()

		log.SetOutput(logFile)
	}

	// 显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		config.DisplayVersion()
	}

	// 初始化配置
	config.InitAppConfig()

	// 清理旧备份
	services.DeleteAllBackups()

	// 创建PID文件和升级器
	pidFile := path.Join(tools.GetPWD(), "uranus.pid")
	upg, err := tableflip.New(tableflip.Options{
		PIDFile: pidFile,
	})
	if err != nil {
		log.Fatalf("无法初始化升级器: %v", err)
	}
	defer upg.Stop()

	// 处理信号
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		for s := range sig {
			switch s {
			case syscall.SIGHUP, syscall.SIGUSR2:
				log.Printf("收到升级信号，开始升级")
				if err := upg.Upgrade(); err != nil {
					log.Printf("升级错误: %v", err)
				}
			case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
				log.Printf("收到关闭信号，准备关闭")
				upg.Stop()
				return
			}
		}
	}()

	// 检查升级触发器
	go checkUpgradeTrigger(upg)

	// 初始化路由
	router := initRouter()

	// 监听端口
	ln, err := upg.Fds.Listen("tcp", "0.0.0.0:7777")
	if err != nil {
		log.Fatalf("无法监听端口: %v", err)
	}

	// 配置服务器
	server := configureServer(router)

	// 启动服务器
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP服务器错误: %v", err)
		}
	}()

	// 初始化数据库
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	models.InitWithContext(ctx)

	// 启动服务
	go services.RenewSSL()
	if gin.Mode() == gin.ReleaseMode {
		go services.HeartbeatWithContext(ctx)
	}

	// 写入PID文件
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0755); err != nil {
		log.Printf("写入PID文件错误: %v", err)
	}

	// 标记进程已准备好
	if err := upg.Ready(); err != nil {
		log.Fatalf("无法标记进程为Ready: %v", err)
	}

	// 等待退出信号
	<-upg.Exit()

	// 优雅关闭服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("服务器关闭错误: %v", err)
	}

	// 删除PID文件
	if err := os.Remove(pidFile); err != nil {
		log.Printf("删除PID文件错误: %v", err)
	}
}
