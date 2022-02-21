package main

import (
	"context"
	"embed"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/middlewares"
	"github.com/qfdk/nginx-proxy-manager/app/routes"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed views
var templates embed.FS

//go:embed public
var staticFS embed.FS

func mustFS() http.FileSystem {
	sub, err := fs.Sub(staticFS, "public")

	if err != nil {
		panic(err)
	}

	return http.FS(sub)
}
func main() {
	// 线上模式显示版本信息
	if gin.Mode() == gin.ReleaseMode {
		displayVersion()
	}
	// 初始化配置文件
	if config.GetAppConfig().Redis {
		// 初始化redis
		config.InitRedis()
		defer config.CloseRedis()
	}

	app := gin.New()
	template, _ := template.ParseFS(templates, "views/includes/*.html", "views/*.html")
	app.SetHTMLTemplate(template)
	// 缓存中间件
	app.Use(middlewares.CacheMiddleware())
	// 静态文件路由
	app.StaticFS("/public", mustFS())
	app.GET("/favicon.ico", func(c *gin.Context) {
		file, _ := staticFS.ReadFile("public/icon/favicon.ico")
		c.Data(http.StatusOK, "image/x-icon", file)
	})
	app.SetTrustedProxies([]string{"127.0.0.1"})
	routes.RegisterRoutes(app)
	go services.RenewSSL()

	srv := &http.Server{
		Addr:    ":7777",
		Handler: app,
	}
	// 平滑启动
	// Initializing the server in a goroutine so that
	// it won't block the graceful shutdown handling below
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[+] 正在关闭服务器...")
	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("[+] 服务器需要强制退出: ", err)
	}
	log.Println("[+] 服务器已退出")
}
