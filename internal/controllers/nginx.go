package controllers

import (
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"net/url"
	"os"
	"uranus/internal/config"
	"uranus/internal/services"
)

func Nginx(ctx *gin.Context) {
	action, ok := ctx.GetPostForm("action")
	if !ok {
		log.Println("No action parameter found")
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Missing action parameter"})
		return
	}

	var nginxActionResult string
	switch action {
	case "start":
		nginxActionResult = services.StartNginx()
	case "reload":
		nginxActionResult = services.ReloadNginx()
	case "restart":
		nginxActionResult = services.RestartNginx()
	case "stop":
		nginxActionResult = services.StopNginx()
	default:
		nginxActionResult = "未知操作：" + action
	}

	// 直接回到仪表盘显示结果：经过 / 再跳转会丢掉查询参数
	query := url.Values{"action": {action}, "message": {nginxActionResult}}
	ctx.Redirect(http.StatusFound, "/admin/dashboard?"+query.Encode())
}

// GetNginxConf shows nginx.conf as it currently is on disk. It may have been
// edited elsewhere (e.g. in the web terminal); showing a cached copy would make
// the next save silently undo those edits.
func GetNginxConf(ctx *gin.Context) {
	nginxConfPath := config.ReadNginxCompileInfo().NginxConfPath
	content, err := os.ReadFile(nginxConfPath)
	if err != nil {
		log.Printf("Failed to read Nginx configuration file: %v", err)
		ctx.String(http.StatusInternalServerError, "Failed to read Nginx configuration file")
		return
	}

	ctx.HTML(http.StatusOK, "nginxEdit.html", gin.H{
		"activePage":         "config",
		"configFileName":     "nginx",
		"content":            string(content),
		"isNginxDefaultConf": true,
	})
}

func SaveNginxConf(ctx *gin.Context) {
	content, _ := ctx.GetPostForm("content")
	result := services.SaveNginxConf(content)
	ctx.JSON(http.StatusOK, gin.H{"message": result})
}

// GetNginxCompileInfo shows how nginx is installed: its build and where
// Uranus found its files.
func GetNginxCompileInfo(ctx *gin.Context) {
	info := config.ReadNginxCompileInfo()
	data := gin.H{
		"activePage":       "config",
		"nginxCompileInfo": info,
		"installed":        info.NginxConfPath != "",
	}
	if info.NginxConfPath != "" {
		data["pidFile"] = config.NginxPidFile()
		data["sites"] = config.Sites()
		data["sslDir"] = config.SSLDir()
		data["manager"] = services.NginxManager()
	}
	ctx.HTML(http.StatusOK, "config.html", data)
}
