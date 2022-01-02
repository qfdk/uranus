package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"net/http"
	"proxy-manager/app/services"
	"proxy-manager/config"
	"strconv"
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
	var memTotal string
	if memInfo.Total >= 1073741824 {
		memTotal = strconv.FormatFloat(float64(memInfo.Total)/1024/1024/1024.0, 'f', 2, 64) + " G"
	} else {
		memTotal = strconv.FormatFloat(float64(memInfo.Total)/1024/1024.0, 'f', 2, 64) + " M"
	}

	ctx.HTML(http.StatusOK, "index",
		gin.H{
			"osName":      fullOsName,
			"cpu":         cpuInfo[0],
			"memInfo":     memTotal,
			"nginxStatus": nginxStatus,
		})
}

func GetNginxCompileInfo(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "config", gin.H{"nginxCompileInfo": config.GetNginxCompileInfo()})
}

func Domains(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "domains", gin.H{})
}

func SSLSettings(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "ssl", gin.H{})
}
