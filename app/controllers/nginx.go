package controllers

import (
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"io/ioutil"
	"log"
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
	case "parser":
		log.Println("读取 nginx 配置文件")
		content, _ := ioutil.ReadFile(config.GetNginxCompileInfo().NginxConfPath)
		ctx.HTML(http.StatusOK, "nginxEdit.html", gin.H{"configFileName": "nginx", "content": string(content), "isNginxDefaultConf": true})
		return
	case "saveConfig":
		content, _ := ctx.GetPostForm("content")
		services.SaveNginxConf(content)
		ctx.JSON(http.StatusOK, gin.H{"message": "OK"})
		return
	}
	ctx.Redirect(http.StatusMovedPermanently, "/?message="+base64.StdEncoding.EncodeToString([]byte(nginxActionResult)))
}
