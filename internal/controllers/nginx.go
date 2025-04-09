package controllers

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
	"sync"
	"uranus/internal/config"
	"uranus/internal/services"
)

var (
	// Cache the nginx configuration content
	nginxConfCache     string
	nginxConfCacheLock sync.RWMutex
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
	case "stop":
		nginxActionResult = services.StopNginx()
	default:
		nginxActionResult = "Unknown action"
	}

	// Encode the result for URL safety
	encodedResult := base64.StdEncoding.EncodeToString([]byte(nginxActionResult))
	ctx.Redirect(http.StatusFound, "/?message="+encodedResult)
}

func GetNginxConf(ctx *gin.Context) {
	log.Println("Reading Nginx configuration file")

	// Check cache first
	nginxConfCacheLock.RLock()
	cachedContent := nginxConfCache
	nginxConfCacheLock.RUnlock()

	var content string
	if cachedContent != "" {
		content = cachedContent
	} else {
		// Cache miss, read from disk
		nginxConfPath := config.ReadNginxCompileInfo().NginxConfPath

		fileContent, err := os.ReadFile(nginxConfPath)
		if err != nil {
			log.Printf("Failed to read Nginx configuration file: %v", err)
			ctx.String(http.StatusInternalServerError, "Failed to read Nginx configuration file")
			return
		}

		content = string(fileContent)

		// Update cache
		nginxConfCacheLock.Lock()
		nginxConfCache = content
		nginxConfCacheLock.Unlock()
	}

	ctx.HTML(http.StatusOK, "nginxEdit.html", gin.H{
		"configFileName":     "nginx",
		"content":            content,
		"isNginxDefaultConf": true,
	})
}

func SaveNginxConf(ctx *gin.Context) {
	content, _ := ctx.GetPostForm("content")

	// Invalidate cache on save
	nginxConfCacheLock.Lock()
	nginxConfCache = ""
	nginxConfCacheLock.Unlock()

	result := services.SaveNginxConf(content)
	ctx.JSON(http.StatusOK, gin.H{"message": result})
}

func GetNginxCompileInfo(ctx *gin.Context) {
	// Get compile info from the shared cache in config package
	nginxCompileInfo := config.ReadNginxCompileInfo()

	ctx.HTML(http.StatusOK, "config.html", gin.H{
		"activePage":       "config",
		"nginxCompileInfo": nginxCompileInfo,
	})
}
