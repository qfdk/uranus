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
	// Production mode writes to log
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
	app.Use(gin.Recovery()) // Add recovery middleware for stability
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

	// Use cache middleware
	app.Use(middlewares.CacheMiddleware())

	cssSubFS, _ := fs.Sub(cssFS, "assets/css")
	app.StaticFS("/assets/css", http.FS(cssSubFS))

	iconsSubFS, _ := fs.Sub(iconsFS, "assets/icons")
	app.StaticFS("/assets/icons", http.FS(iconsSubFS))

	// Set static file routes
	app.StaticFS("/public", mustFS())

	// Handle favicon.ico requests
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, err := staticFS.ReadFile("web/public/icon/favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", file)
	})

	// Set trusted proxies
	err = app.SetTrustedProxies([]string{"127.0.0.1"})
	if err != nil {
		return nil
	}

	// Register routes
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

	// Create a context that will be canceled on shutdown signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

		// 定期检查触发文件
		triggerCheck := time.NewTicker(5 * time.Second)
		defer triggerCheck.Stop()

		for {
			select {
			case s := <-sig:
				switch s {
				case syscall.SIGHUP, syscall.SIGUSR2:
					log.Printf("[PID][%d]: Received upgrade signal, starting upgrade", os.Getpid())
					err := upg.Upgrade()
					if err != nil {
						log.Printf("[PID][%d]: Upgrade error, %s", os.Getpid(), err)
						continue
					}
					log.Printf("[PID][%d]: Upgrade complete", os.Getpid())
				case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
					log.Printf("[PID][%d]: Received shutdown signal, preparing to close server", os.Getpid())
					cancel() // Cancel the context to signal goroutines to stop
					upg.Stop()
					log.Printf("[PID][%d]: Server fully closed", os.Getpid())
					os.Exit(0)
				}
			case <-triggerCheck.C:
				triggerFile := path.Join(tools.GetPWD(), ".upgrade_trigger")
				if _, err := os.Stat(triggerFile); err == nil {
					// 触发文件存在，执行升级
					log.Printf("[PID][%d]: Found upgrade trigger file, starting upgrade", os.Getpid())
					os.Remove(triggerFile) // 删除触发文件

					err := upg.Upgrade()
					if err != nil {
						log.Printf("[PID][%d]: Upgrade error, %s", os.Getpid(), err)
					}
				}
			}
		}
	}()

	ln, err := upg.Fds.Listen("tcp", "0.0.0.0:7777")
	if err != nil {
		log.Fatalln("Unable to listen:", err)
	}

	// Configure server with timeouts and other performance settings
	server := &http.Server{
		Handler:           initRouter(),
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Start the server in a goroutine
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Println("HTTP server:", err)
		}
	}()

	// Initialize SQLite database with context
	dbCtx, dbCancel := context.WithCancel(ctx)
	defer dbCancel()
	models.InitWithContext(dbCtx)

	go services.RenewSSL()

	// Start heartbeat service with context if in release mode
	if gin.Mode() == gin.ReleaseMode {
		heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
		defer heartbeatCancel()
		go services.HeartbeatWithContext(heartbeatCtx)
	}

	log.Printf("[PID][%d]: Server started successfully and wrote PID to file", os.Getpid())
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0755); err != nil {
		log.Println("Error writing PID file:", err)
	}

	if err := upg.Ready(); err != nil {
		panic(err)
	}
	<-upg.Exit()

	// Setup shutdown timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown the server gracefully
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Println("Server shutdown error:", err)
	}

	log.Println("Exiting and deleting pid file")
	if err := os.Remove(pidFile); err != nil {
		log.Println("Error deleting PID file:", err)
	}
}

func main() {
	// Show version info in production mode
	if gin.Mode() == gin.ReleaseMode {
		config.DisplayVersion()
	}

	// Initialize configuration file
	config.InitAppConfig()

	// Start the server with graceful shutdown
	Graceful()
}
