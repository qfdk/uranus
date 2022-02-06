package controllers

import (
	"encoding/base64"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	. "github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"net/http"
)

func Index(ctx *gin.Context) {
	// nginx status
	var actionMessage = ctx.Query("message")
	actionMessageDec, err := base64.StdEncoding.DecodeString(actionMessage)
	if err != nil {
		fmt.Printf("base64 decode failure, error=[%v]\n", err)
	}
	// host
	hostInfo, _ := host.Info()
	var fullOsName string
	if hostInfo.Platform == "darwin" {
		fullOsName = "macOS " + hostInfo.PlatformVersion
	} else {
		fullOsName = hostInfo.Platform + " " + hostInfo.PlatformVersion
	}
	// cpu
	cpuInfo, _ := cpu.Info()
	// memory
	memInfo, _ := mem.VirtualMemory()

	ctx.HTML(http.StatusOK, "index.html",
		gin.H{
			"osName":             fullOsName,
			"cpu":                cpuInfo[0],
			"memInfo":            humanize.Bytes(memInfo.Total),
			"nginxStatus":        services.NginxStatus(),
			"nginxActionMessage": string(actionMessageDec),
			"nginxCompileInfo":   ReadNginxCompileInfo(),
		})
}
