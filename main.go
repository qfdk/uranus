package main

import (
	"context"
	"embed"
	"errors"
	"github.com/cloudflare/tableflip"
	"github.com/gin-gonic/gin"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
	"uranus/internal/config"
	"uranus/internal/middlewares"
	"uranus/internal/models"
	"uranus/internal/mqtty"
	"uranus/internal/routes"
	"uranus/internal/services"
	"uranus/internal/tools"
)

//go:embed web
var templates embed.FS

//go:embed web/public
var staticFS embed.FS

func init() {
	// 生产模式写入日志
	if gin.Mode() == gin.ReleaseMode {
		logDir := path.Join(tools.GetPWD(), "logs")
		if err := os.MkdirAll(logDir, 0755); err != nil && !os.IsExist(err) {
			panic(err)
		}

		logFile := path.Join(logDir, "app.log")
		file, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			panic(err)
		}

		wrt := io.MultiWriter(os.Stdout, file)
		log.SetOutput(wrt)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
}

func initRouter() *gin.Engine {
	app := gin.New()
	app.Use(gin.Recovery())

	// 创建一个包含自定义函数的模板引擎
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

	// 使用自定义函数创建一个模板实例
	tmpl := template.New("").Funcs(funcMap)

	// 解析模板文件
	tmpl, err := tmpl.ParseFS(templates, "web/includes/*.html", "web/*.html")
	if err != nil {
		panic(err)
	}

	app.SetHTMLTemplate(tmpl)

	// 使用缓存中间件
	app.Use(middlewares.CacheMiddleware())

	// 设置静态文件服务，直接使用 embed.FS
	app.Use(func(c *gin.Context) {
		// 处理静态文件请求
		if strings.HasPrefix(c.Request.URL.Path, "/public/") {
			// 移除 /public/ 前缀
			filePath := strings.TrimPrefix(c.Request.URL.Path, "/public/")

			// 尝试读取文件
			file, err := staticFS.ReadFile(path.Join("web/public", filePath))
			if err == nil {
				// 根据文件扩展名设置正确的 MIME 类型
				contentType := getMimeType(filePath)
				c.Data(http.StatusOK, contentType, file)
				c.Abort()
				return
			}
		}
		c.Next()
	})

	// 处理favicon.ico
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, err := staticFS.ReadFile("web/public/icon/favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", file)
	})

	// 设置受信任的代理
	err = app.SetTrustedProxies([]string{"127.0.0.1"})
	if err != nil {
		return nil
	}

	// 注册路由
	routes.RegisterRoutes(app)

	return app
}

// 根据文件扩展名获取 MIME 类型
func getMimeType(filePath string) string {
	ext := path.Ext(filePath)
	switch ext {
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".ico":
		return "image/x-icon"
	case ".html":
		return "text/html"
	default:
		return "application/octet-stream"
	}
}

func Graceful() {
	pidFile := path.Join(tools.GetPWD(), "uranus.pid")
	upg, err := tableflip.New(tableflip.Options{
		PIDFile: pidFile,
	})
	if err != nil {
		panic(err)
	}
	defer upg.Stop()

	// 创建一个将在关闭信号时取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理信号
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		for {
			select {
			case s := <-sig:
				switch s {
				case syscall.SIGHUP, syscall.SIGUSR2:
					log.Printf("[进程][%d]: 收到升级信号，开始升级", os.Getpid())
					err := upg.Upgrade()
					if err != nil {
						log.Printf("[进程][%d]: 升级错误，%s", os.Getpid(), err)
						continue
					}
					log.Printf("[进程][%d]: 升级完成", os.Getpid())
				case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
					log.Printf("[进程][%d]: 收到关闭信号，准备关闭服务器", os.Getpid())
					upg.Stop()
					log.Printf("[进程][%d]: 服务器完全关闭", os.Getpid())
					os.Exit(0)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 创建触发检查计时器
	triggerCheck := time.NewTicker(5 * time.Second)
	defer triggerCheck.Stop()

	// 触发文件路径
	triggerPaths := []string{
		path.Join("/etc/uranus", ".upgrade_trigger"),
		path.Join(tools.GetPWD(), ".upgrade_trigger"),
	}

	// 启动时检查并删除可能存在的触发文件
	for _, filePath := range triggerPaths {
		if _, err := os.Stat(filePath); err == nil {
			location := "标准安装目录"
			if filePath == triggerPaths[1] {
				location = "工作目录"
			}
			log.Printf("[进程][%d]: 启动时发现%s触发文件，正在删除", os.Getpid(), location)
			os.Remove(filePath)
		}
	}

	log.Printf("[进程][%d]: 应用启动，清理所有旧备份文件", os.Getpid())
	services.DeleteAllBackups()

	// 处理重启服务的函数
	restartService := func(triggerPath string) {
		location := "升级"
		if triggerPath == triggerPaths[1] {
			location = "工作目录中的升级"
		}

		log.Printf("[进程][%d]: 发现%s触发文件，立即删除", os.Getpid(), location)

		if err := os.Remove(triggerPath); err != nil {
			log.Printf("[进程][%d]: 删除触发文件失败: %v", os.Getpid(), err)
		} else {
			log.Printf("[进程][%d]: 已删除触发文件", os.Getpid())
		}

		log.Printf("[进程][%d]: 执行systemctl restart %s", os.Getpid(), "uranus.service")
		restartCmd := exec.Command("systemctl", "restart", "uranus.service")
		output, err := restartCmd.CombinedOutput()

		if err != nil {
			log.Printf("[进程][%d]: 重启命令失败: %v, 输出: %s", os.Getpid(), err, string(output))
		} else {
			log.Printf("[进程][%d]: 重启命令已执行", os.Getpid())
		}
	}

	// 升级触发文件检查
	go func() {
		log.Printf("[进程][%d]: 启动升级触发文件检查器，每5秒检查一次", os.Getpid())
		checkCount := 0

		for {
			select {
			case <-triggerCheck.C:
				checkCount++

				for _, triggerPath := range triggerPaths {
					exists, _ := tools.FileExists(triggerPath)
					if exists {
						restartService(triggerPath)
						log.Printf("[进程][%d]: 清理所有旧备份文件", os.Getpid())
						services.DeleteAllBackups()
						break // 找到一个触发文件后立即处理并停止检查其他路径
					}
				}
			case <-ctx.Done():
				log.Printf("[进程][%d]: 升级检查器收到停止信号，总共执行了%d次检查", os.Getpid(), checkCount)
				return
			}
		}
	}()

	ln, err := upg.Fds.Listen("tcp", "0.0.0.0:7777")
	if err != nil {
		log.Fatalln("无法监听端口:", err)
	}

	// 配置服务器
	server := &http.Server{
		Handler:           initRouter(),
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// 启动服务器
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("HTTP服务器错误:", err)
		}
	}()

	// 初始化数据库
	dbCtx, dbCancel := context.WithCancel(ctx)
	defer dbCancel()
	models.InitWithContext(dbCtx)

	go services.RenewSSL()

	// MQTT心跳服务现在已经集成到mqtty模块中

	// 启动MQTT终端服务
	mqttyOpts := mqtty.DefaultOptions()
	mqttyServer := mqtty.NewTerminal(mqttyOpts)
	err = mqttyServer.Start()
	if err != nil {
		log.Printf("[进程][%d]: MQTT终端服务启动失败: %v", os.Getpid(), err)
	} else {
		log.Printf("[进程][%d]: MQTT终端服务已启动", os.Getpid())
	}
	defer mqttyServer.Stop()

	log.Printf("[进程][%d]: 服务器启动成功并将PID写入文件", os.Getpid())
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0755); err != nil {
		log.Println("写入PID文件错误:", err)
	}

	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()

	// 关闭服务器
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Println("服务器关闭错误:", err)
	}

	log.Println("退出并删除pid文件")
	if err := os.Remove(pidFile); err != nil {
		log.Println("删除PID文件错误:", err)
	}
}

func main() {
	// 检查命令行参数是否包含 --version 或 -v
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			config.DisplayVersion()
			return
		}
	}

	// 在生产模式下显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		config.DisplayVersion()
	}

	// 初始化配置文件
	config.InitAppConfig()

	// 启动带有优雅关闭的服务器
	Graceful()
}
