package controllers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"proxy-manager/app/services"
	"fmt"
	"log"
)

func Nginx(ctx *gin.Context) {
	action, ok := ctx.GetPostForm("action")
	if !ok {
		// 参数不存在
		fmt.Println("参数不存在")
	}
	var _ string
	switch action {
	case "start":
		log.Println("启动 nginx")
		_ = services.StartNginx()
	case "reload":
		log.Println("重载 nginx 配置文件")
		_ = services.ReloadNginx()
	case "stop":
		log.Println("关闭 nginx")
		_ = services.StopNginx()
	}

	ctx.Redirect(http.StatusMovedPermanently, "/")
}
