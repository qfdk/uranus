package controllers

import (
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"io/ioutil"
	"net/http"
)

func Nginx(ctx *gin.Context) {
	action, ok := ctx.GetPostForm("action")
	if !ok {
		// 参数不存在
		fmt.Println("参数不存在")
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
	ctx.Redirect(http.StatusMovedPermanently, "/?message="+base64.StdEncoding.EncodeToString([]byte(nginxActionResult)))
}

func GetNginxConf(ctx *gin.Context) {
	fmt.Println("读取 Nginx 配置文件")
	content, _ := ioutil.ReadFile(config.ReadNginxCompileInfo().NginxConfPath)
	ctx.HTML(http.StatusOK, "nginxEdit.html", gin.H{"configFileName": "nginx", "content": string(content), "isNginxDefaultConf": true})
}

func SaveNginxConf(ctx *gin.Context) {
	content, _ := ctx.GetPostForm("content")
	services.SaveNginxConf(content)
	ctx.JSON(http.StatusOK, gin.H{"message": "OK"})
}

func GetNginxCompileInfo(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "config.html", gin.H{"nginxCompileInfo": config.ReadNginxCompileInfo()})
}
