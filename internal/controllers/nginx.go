package controllers

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
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
	case "stop":
		nginxActionResult = services.StopNginx()
	default:
		nginxActionResult = "Unknown action"
	}

	// Encode the result for URL safety
	encodedResult := base64.StdEncoding.EncodeToString([]byte(nginxActionResult))
	ctx.Redirect(http.StatusFound, "/?message="+encodedResult)
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

func GetNginxCompileInfo(ctx *gin.Context) {
	// Get compile info from the shared cache in config package
	nginxCompileInfo := config.ReadNginxCompileInfo()

	ctx.HTML(http.StatusOK, "config.html", gin.H{
		"activePage":       "config",
		"nginxCompileInfo": nginxCompileInfo,
	})
}
