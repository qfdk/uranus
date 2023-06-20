package main

import (
	"context"
	"embed"
	"github.com/cloudflare/tableflip"
	"github.com/gin-gonic/gin"
	"html/template"
	"io"
	"io/fs"
	"io/ioutil"
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
)

//go:embed web
var templates embed.FS

//go:embed web/public
var staticFS embed.FS

func init() {
	// 生产模式写入日志
	if gin.Mode() == gin.ReleaseMode {
		if _, err := os.Stat(path.Join(tools.GetPWD(), "logs")); os.IsNotExist(err) {
			os.MkdirAll(path.Join(tools.GetPWD(), "logs"), 0755)
		}
		file := path.Join(tools.GetPWD(), "logs", "app.log")
		logFile, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0766)
		if err != nil {
			panic(err)
		}
		wrt := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(wrt)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	return
}

func mustFS() http.FileSystem {
	sub, err := fs.Sub(staticFS, "web/public")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

func initRouter() *gin.Engine {
	app := gin.New()
	template, _ := template.ParseFS(templates, "web/includes/*.html", "web/*.html")
	app.SetHTMLTemplate(template)
	// 缓存中间件
	app.Use(middlewares.CacheMiddleware())
	// 静态文件路由
	app.StaticFS("/public", mustFS())
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := staticFS.ReadFile("web/public/icon/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", file)
	})
	app.SetTrustedProxies([]string{"127.0.0.1"})
	routes.RegisterRoutes(app)
	return app
}

func Graceful() {
	var pidFile = path.Join(tools.GetPWD(), "uranus.pid")
	upg, err := tableflip.New(tableflip.Options{
		PIDFile: pidFile,
	})
	if err != nil {
		panic(err)
	}
	defer upg.Stop()

	// Do an upgrade on SIGHUP
	var exit bool
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		for s := range sig {
			switch s {
			case syscall.SIGHUP, syscall.SIGUSR2:
				log.Printf("[PID][%d]: 收到升级信号, 升级开始", os.Getpid())
				err := upg.Upgrade()
				if err != nil {
					log.Printf("[PID][%d]: 升级出错, %s", os.Getpid(), err)
					continue
				} else {
					log.Printf("[PID][%d]: 升级完成", os.Getpid())
				}
			case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
				log.Printf("[PID][%d]: 收到关闭信号, 准备关闭服务器", os.Getpid())
				exit = true
				upg.Stop()
				log.Printf("[PID][%d]: 服务器完全关闭", os.Getpid())
			}
		}
	}()

	ln, err := upg.Fds.Listen("tcp", "0.0.0.0:7777")
	if err != nil {
		log.Fatalln("无法监听:", err)
	}

	server := &http.Server{
		Handler: initRouter(),
	}

	go func() {
		err := server.Serve(ln)
		if err != http.ErrServerClosed {
			log.Println("HTTP server:", err)
		}
	}()
	log.Printf("[PID][%d]: 服务器启动成功并写入 PID 到文件", os.Getpid())
	ioutil.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0755)
	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()

	// Make sure to set a deadline on exiting the process
	// after upg.Exit() is closed. No new upgrades can be
	// performed if the parent doesn't exit.
	time.AfterFunc(10*time.Second, func() {
		log.Println("平滑关闭超时 ...")
		os.Exit(1)
	})
	// Wait for connections to drain.
	server.Shutdown(context.Background())

	if exit {
		log.Println("退出并删除pid文件")
		_ = os.Remove(pidFile)
	}
}

func main() {
	// 线上模式显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		config.DisplayVersion()
	}
	// 初始化配置文件
	config.InitAppConfig()
	// 初始化 SQLite 数据库
	go models.Init()
	// 初始化 自动签名
	go services.RenewSSL()
	initRouter()
	Graceful()
}
