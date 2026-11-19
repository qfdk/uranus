package controllers

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"net/http"
	"runtime"
	"time"
	config2 "uranus/internal/config"
	"uranus/internal/models"
	"uranus/internal/services"
)

// usage 是仪表盘上一项资源的使用情况
type usage struct {
	Used    string
	Total   string
	Percent float64
	Level   string // ok / warn / high，决定进度条颜色
}

func newUsage(used, total uint64, percent float64) *usage {
	level := "ok"
	switch {
	case percent >= 90:
		level = "high"
	case percent >= 75:
		level = "warn"
	}
	return &usage{Used: humanize.IBytes(used), Total: humanize.IBytes(total), Percent: percent, Level: level}
}

// formatUptime 把开机秒数写成“3 天 4 小时”这样的形式
func formatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := seconds % 86400 / 3600
	if days > 0 {
		return fmt.Sprintf("%d 天 %d 小时", days, hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, seconds%3600/60)
}

// shortCommit 返回提交号的前 8 位。从不带 .git 的源码构建时提交号为空
func shortCommit(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func Index(ctx *gin.Context) {
	nginxInfo := config2.ReadNginxCompileInfo()
	data := gin.H{
		"activePage":     "dashboard",
		"nginxInstalled": nginxInfo.NginxConfPath != "",
		"nginxStatus":    services.NginxStatus(),
		// 启动、重载、重启、停止 Nginx 之后带回来的结果
		"nginxAction":        ctx.Query("action"),
		"nginxActionMessage": ctx.Query("message"),
		"nginxCompileInfo":   nginxInfo,
		"buildTime":          config2.BuildTime,
		"buildVersion":       config2.BuildVersion,
		"commitID":           config2.CommitID,
		"commitShort":        shortCommit(config2.CommitID),
		"cpuCount":           runtime.NumCPU(),
	}

	// 以下信息取不到时页面上对应位置留空，而不是报错
	if hostInfo, err := host.Info(); err == nil && hostInfo != nil {
		osName := hostInfo.Platform + " " + hostInfo.PlatformVersion
		if hostInfo.Platform == "darwin" {
			osName = "macOS " + hostInfo.PlatformVersion
		}
		data["osName"] = osName
		data["hostname"] = hostInfo.Hostname
		data["kernel"] = hostInfo.KernelVersion
		data["uptime"] = formatUptime(hostInfo.Uptime)
	}
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		data["cpuModel"] = cpuInfo[0].ModelName
	}
	if avg, err := load.Avg(); err == nil && avg != nil {
		data["load"] = []string{
			fmt.Sprintf("%.2f", avg.Load1), fmt.Sprintf("%.2f", avg.Load5), fmt.Sprintf("%.2f", avg.Load15),
		}
	}
	if memInfo, err := mem.VirtualMemory(); err == nil && memInfo != nil {
		data["mem"] = newUsage(memInfo.Used, memInfo.Total, memInfo.UsedPercent)
	}
	if diskInfo, err := disk.Usage("/"); err == nil && diskInfo != nil {
		data["disk"] = newUsage(diskInfo.Used, diskInfo.Total, diskInfo.UsedPercent)
	}

	// 网站和证书概况
	sites := len(config2.SiteFiles())
	certs := models.IssuedCertificates()
	expiring := 0
	for _, cert := range certs {
		if time.Until(cert.NotAfter) < 30*24*time.Hour {
			expiring++
		}
	}
	data["sitesCount"] = sites
	data["certsCount"] = len(certs)
	data["certsExpiring"] = expiring

	ctx.HTML(http.StatusOK, "index.html", data)
}
