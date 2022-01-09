package controllers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"proxy-manager/app/services"
	"proxy-manager/config"
)

func Nginx(ctx *gin.Context) {
	action, ok := ctx.GetPostForm("action")
	if !ok {
		// 参数不存在
		fmt.Println("参数不存在")
	}
	switch action {
	case "start":
		services.StartNginx()
	case "reload":
		services.ReloadNginx()
	case "stop":
		services.StopNginx()
	case "parser":
		log.Println("读取 nginx 配置文件")
		content, _ := ioutil.ReadFile(config.GetNginxCompileInfo().NginxConfPath)
		ctx.HTML(http.StatusOK, "edit", gin.H{"configFileName": "nginx.conf", "content": string(content), "disabledChangeFileName": true})
		return
	case "saveConfig":
		content, _ := ctx.GetPostForm("content")
		services.SaveNginxConf(content)
		ctx.JSON(http.StatusOK, gin.H{"message": "OK"})
		return
	}
	ctx.Redirect(http.StatusMovedPermanently, "/")
}
