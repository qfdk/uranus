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
	"regexp"
	"runtime"
	"slices"
	"strings"
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

// cpuNoise 是 CPU 名称里不影响辨认的部分：商标、主频、核显
var cpuNoise = regexp.MustCompile(`\((R|TM|tm)\)| @ .*$| with .* Graphics$| Processor$`)

// shortCPUModel 把 CPU 名称缩短到能和标签放在一行，例如
// "Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz" 写成 "Intel Xeon E5-2680 v4"
func shortCPUModel(model string) string {
	short := cpuNoise.ReplaceAllString(model, "")
	fields := strings.Fields(short)
	// Intel 的名称里多一个 CPU；"QEMU Virtual CPU" 这种名字里它有意义，留着
	if len(fields) > 0 && fields[0] == "Intel" {
		fields = slices.DeleteFunc(fields, func(f string) bool { return f == "CPU" })
	}
	return strings.Join(fields, " ")
}

// shortCompiler 只留编译器和版本，括号里的发行版打包信息在编译参数页上看
func shortCompiler(version string) string {
	short, _, _ := strings.Cut(version, " (")
	return short
}

// shortSSL 去掉 OpenSSL 版本后面的发布日期："OpenSSL 3.0.2 15 Mar 2022" 写成 "OpenSSL 3.0.2"
func shortSSL(version string) string {
	fields := strings.Fields(version)
	if len(fields) > 2 && fields[1] != "" && fields[1][0] >= '0' && fields[1][0] <= '9' {
		return fields[0] + " " + fields[1]
	}
	return version
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
		"compiler":           shortCompiler(nginxInfo.CompilerVersion),
		"ssl":                shortSSL(nginxInfo.SSLVersion),
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
		data["cpuModel"] = shortCPUModel(cpuInfo[0].ModelName)
		data["cpuModelFull"] = cpuInfo[0].ModelName
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
