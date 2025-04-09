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

				foundTrigger := false
				for _, triggerPath := range triggerPaths {
					exists, _ := fileExists(triggerPath)
					if exists {
						restartService(triggerPath)
						log.Printf("[进程][%d]: 清理所有旧备份文件", os.Getpid())
						services.DeleteAllBackups()
						foundTrigger = true
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
