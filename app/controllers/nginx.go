package controllers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
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
		log.Println("启动 nginx")
		services.StartNginx()
	case "reload":
		log.Println("重载 nginx 配置文件")
		services.ReloadNginx()
	case "stop":
		log.Println("关闭 nginx")
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
	case "template":
		log.Println("读取 nginx 模板配置文件")
		content, err := ioutil.ReadFile(filepath.Join("template", "http.conf"))
		if err != nil {
			fmt.Println(err)
		}
		ctx.HTML(http.StatusOK, "edit", gin.H{"configFileName": "blog.qfdk.me.conf", "content": string(content)})
		return
	}
	ctx.Redirect(http.StatusMovedPermanently, "/")
}
