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
		// 参数不存在
		log.Println("参数不存在")
	}
	var nginxActionResult string
	switch action {
	case "start":
		nginxActionResult = services.StartNginx()
	case "reload":
		nginxActionResult = services.ReloadNginx()
	case "stop":
		nginxActionResult = services.StopNginx()
	}
	ctx.Redirect(http.StatusFound, "/?message="+base64.StdEncoding.EncodeToString([]byte(nginxActionResult)))
}

func GetNginxConf(ctx *gin.Context) {
	log.Println("读取 Nginx 配置文件")
	nginxConfPath := config.ReadNginxCompileInfo().NginxConfPath

	content, err := os.ReadFile(nginxConfPath)
	if err != nil {
		log.Printf("读取 Nginx 配置文件失败: %v", err)
		ctx.String(http.StatusInternalServerError, "读取 Nginx 配置文件失败")
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
	ctx.JSON(http.StatusOK, gin.H{"message": services.SaveNginxConf(content)})
}

func GetNginxCompileInfo(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "config.html", gin.H{"nginxCompileInfo": config.ReadNginxCompileInfo()})
}
