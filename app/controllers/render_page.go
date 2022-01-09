package controllers

import (
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"net/http"
	"proxy-manager/app/services"
	"proxy-manager/config"
)

func Index(ctx *gin.Context) {
	// nginx status
	nginxStatus := services.NginxStatus()
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

	ctx.HTML(http.StatusOK, "index",
		gin.H{
			"osName":           fullOsName,
			"cpu":              cpuInfo[0],
			"memInfo":          humanize.Bytes(memInfo.Total),
			"nginxStatus":      nginxStatus,
			"nginxCompileInfo": config.GetNginxCompileInfo(),
		})
}

func GetNginxCompileInfo(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "config", gin.H{"nginxCompileInfo": config.GetNginxCompileInfo()})
}
