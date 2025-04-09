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
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
	"uranus/internal/config"
	"uranus/internal/middlewares"
	"uranus/internal/models"
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

	if gin.Mode() == gin.ReleaseMode {
		heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
		defer heartbeatCancel()
		go services.HeartbeatWithContext(heartbeatCtx)
	}

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
	// 在生产模式下显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		config.DisplayVersion()
	}

	// 初始化配置文件
	config.InitAppConfig()

	// 启动带有优雅关闭的服务器
	Graceful()
}
