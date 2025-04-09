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
	"os/exec"
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

	// 启动时检查并删除可能存在的触发文件
	triggerFile := path.Join("/etc/uranus", ".upgrade_trigger")
	if _, err := os.Stat(triggerFile); err == nil {
		log.Printf("[进程][%d]: 启动时发现触发文件，正在删除", os.Getpid())
		os.Remove(triggerFile)
	}

	workDirTrigger := path.Join(tools.GetPWD(), ".upgrade_trigger")
	if _, err := os.Stat(workDirTrigger); err == nil {
		log.Printf("[进程][%d]: 启动时发现工作目录触发文件，正在删除", os.Getpid())
		os.Remove(workDirTrigger)
	}

	// 单独的goroutine检查升级触发文件
	go func() {
		log.Printf("[进程][%d]: 启动升级触发文件检查器，每5秒检查一次", os.Getpid())
		checkCount := 0

		for {
			select {
			case <-triggerCheck.C:
				checkCount++
				if checkCount%12 == 0 { // 每分钟记录一次
					log.Printf("[进程][%d]: 升级检查器运行中，完成%d次检查", os.Getpid(), checkCount)
				}

				// 检查标准安装目录中的触发文件
				triggerFile := path.Join("/etc/uranus", ".upgrade_trigger")
				exists, err := fileExists(triggerFile)

				if err != nil {
					log.Printf("[进程][%d]: 检查触发文件出错: %v", os.Getpid(), err)
				} else if exists {
					log.Printf("[进程][%d]: 发现升级触发文件，立即删除", os.Getpid())

					// 先删除触发文件，再执行重启
					err := os.Remove(triggerFile)
					if err != nil {
						log.Printf("[进程][%d]: 删除触发文件失败: %v", os.Getpid(), err)
					} else {
						log.Printf("[进程][%d]: 已删除触发文件", os.Getpid())
					}

					// 执行重启命令
					log.Printf("[进程][%d]: 执行systemctl restart %s", os.Getpid(), "uranus.service")
					restartCmd := exec.Command("systemctl", "restart", "uranus.service")
					output, err := restartCmd.CombinedOutput()

					if err != nil {
						log.Printf("[进程][%d]: 重启命令失败: %v, 输出: %s", os.Getpid(), err, string(output))
					} else {
						log.Printf("[进程][%d]: 重启命令已执行", os.Getpid())
					}
				}

				// 检查工作目录中的触发文件
				workDirTrigger := path.Join(tools.GetPWD(), ".upgrade_trigger")
				workDirExists, workDirErr := fileExists(workDirTrigger)

				if workDirErr != nil {
					log.Printf("[进程][%d]: 检查工作目录触发文件出错: %v", os.Getpid(), workDirErr)
				} else if workDirExists {
					log.Printf("[进程][%d]: 发现工作目录中的升级触发文件，立即删除", os.Getpid())

					// 先删除触发文件，再执行重启
					err := os.Remove(workDirTrigger)
					if err != nil {
						log.Printf("[进程][%d]: 删除触发文件失败: %v", os.Getpid(), err)
					} else {
						log.Printf("[进程][%d]: 已删除触发文件", os.Getpid())
					}

					// 执行重启命令
					log.Printf("[进程][%d]: 执行systemctl restart %s", os.Getpid(), "uranus.service")
					restartCmd := exec.Command("systemctl", "restart", "uranus.service")
					output, err := restartCmd.CombinedOutput()

					if err != nil {
						log.Printf("[进程][%d]: 重启命令失败: %v, 输出: %s", os.Getpid(), err, string(output))
					} else {
						log.Printf("[进程][%d]: 重启命令已执行", os.Getpid())
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

// 检查文件是否存在
func fileExists(filepath string) (bool, error) {
	_, err := os.Stat(filepath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
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
