package controllers

import (
	"net/http"
	"os"
	"slices"
	"strconv"
	"uranus/internal/services"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
)

// NginxLogs shows the end of a log file of nginx.
func NginxLogs(ctx *gin.Context) {
	files := services.NginxLogFiles()
	selected := ctx.Query("file")
	if !slices.Contains(files, selected) {
		selected = ""
		if len(files) > 0 {
			selected = files[0]
		}
	}
	ctx.HTML(http.StatusOK, "logs.html", gin.H{
		"activePage": "logs",
		"files":      files,
		"selected":   selected,
	})
}

// TailNginxLog returns the last lines of a log file of nginx. Only the files
// the nginx configuration writes to can be read.
func TailNginxLog(ctx *gin.Context) {
	file := ctx.Query("file")
	if !services.IsNginxLogFile(file) {
		ctx.JSON(http.StatusNotFound, gin.H{"message": "这不是 Nginx 写的日志文件"})
		return
	}
	n, err := strconv.Atoi(ctx.DefaultQuery("lines", "200"))
	if err != nil {
		n = 200
	}
	n = min(max(n, 1), 5000)
	lines, err := services.TailFile(file, n)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "读取日志失败：" + err.Error()})
		return
	}
	result := gin.H{"lines": lines}
	if info, err := os.Stat(file); err == nil {
		result["size"] = humanize.IBytes(uint64(info.Size()))
		result["time"] = info.ModTime().Format("2006-01-02 15:04:05")
	}
	ctx.JSON(http.StatusOK, result)
}
