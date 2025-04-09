package main

import (
	"context"
	"embed"
	"errors"
	"github.com/cloudflare/tableflip"
	"github.com/gin-gonic/gin"
	"html/template"
	"io"
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
)

//go:embed web
var templates embed.FS

//go:embed web/public
var staticFS embed.FS

//go:embed assets/css
var cssFS embed.FS

//go:embed assets/icons
var iconsFS embed.FS

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

func mustFS() http.FileSystem {
	sub, err := fs.Sub(staticFS, "web/public")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

func initRouter() *gin.Engine {
	app := gin.New()
	app.Use(gin.Recovery()) // 添加恢复中间件以提高稳定性
	// 创建一个包含自定义函数的模板引擎
	funcMap := template.FuncMap{
		"isEven": func(num int) bool {
			return num%2 == 0
		},
		"add": func(a, b int) int {
			return a + b
		},
		"svgIcon": func(name string) template.HTML {
			content, err := fs.ReadFile(iconsFS, "assets/icons/"+name+".svg")
			if err != nil {
				return template.HTML("")
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

	cssSubFS, _ := fs.Sub(cssFS, "assets/css")
	app.StaticFS("/assets/css", http.FS(cssSubFS))

	iconsSubFS, _ := fs.Sub(iconsFS, "assets/icons")
	app.StaticFS("/assets/icons", http.FS(iconsSubFS))

	// 设置静态文件路由
	app.StaticFS("/public", mustFS())

	// 处理favicon.ico请求
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

// 监控升级函数
func monitorForUpgrades(upg *tableflip.Upgrader, triggerCheck *time.Ticker) {
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
		case <-triggerCheck.C:
			// 检查升级触发文件 - 首先检查固定安装目录
			triggerFile := path.Join("/etc/uranus", ".upgrade_trigger")
			if _, err := os.Stat(triggerFile); err == nil {
				log.Printf("[进程][%d]: 发现安装目录中的升级触发文件，开始处理升级", os.Getpid())

				// 先尝试重启服务
				if services.CheckAndRestartAfterUpgrade() {
					log.Printf("[进程][%d]: 服务重启成功", os.Getpid())
					// 删除触发文件
					os.Remove(triggerFile)
					continue
				}

				// 如果服务重启失败，尝试进程内升级
				os.Remove(triggerFile) // 删除触发文件
				log.Printf("[进程][%d]: 服务重启失败，尝试进程内升级", os.Getpid())

				err := upg.Upgrade()
				if err != nil {
					log.Printf("[进程][%d]: 进程内升级失败，错误：%s", os.Getpid(), err)
				} else {
					log.Printf("[进程][%d]: 进程内升级成功启动", os.Getpid())
				}
			}

			// 同时检查工作目录中的触发文件
			triggerFileAlt := path.Join(tools.GetPWD(), ".upgrade_trigger")
			if _, err := os.Stat(triggerFileAlt); err == nil {
				log.Printf("[进程][%d]: 发现工作目录中的升级触发文件，开始处理升级", os.Getpid())

				// 先尝试重启服务
				if services.CheckAndRestartAfterUpgrade() {
					log.Printf("[进程][%d]: 服务重启成功", os.Getpid())
					// 删除触发文件
					os.Remove(triggerFileAlt)
					continue
				}

				// 如果服务重启失败，尝试进程内升级
				os.Remove(triggerFileAlt) // 删除触发文件
				log.Printf("[进程][%d]: 服务重启失败，尝试进程内升级", os.Getpid())

				err := upg.Upgrade()
				if err != nil {
					log.Printf("[进程][%d]: 进程内升级失败，错误：%s", os.Getpid(), err)
				} else {
					log.Printf("[进程][%d]: 进程内升级成功启动", os.Getpid())
				}
			}
		}
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
		// 设置触发检查计时器
		triggerCheck := time.NewTicker(5 * time.Second)
		defer triggerCheck.Stop()

		// 在goroutine中启动信号/升级监控
		go monitorForUpgrades(upg, triggerCheck)
	}()

	ln, err := upg.Fds.Listen("tcp", "0.0.0.0:7777")
	if err != nil {
		log.Fatalln("无法监听端口:", err)
	}

	// 配置服务器超时和其他性能设置
	server := &http.Server{
		Handler:           initRouter(),
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// 在goroutine中启动服务器
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("HTTP服务器错误:", err)
		}
	}()

	// 使用上下文初始化SQLite数据库
	dbCtx, dbCancel := context.WithCancel(ctx)
	defer dbCancel()
	models.InitWithContext(dbCtx)

	go services.RenewSSL()

	// 如果在发布模式下，启动心跳服务
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

	// 设置关闭超时
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 优雅关闭服务器
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
