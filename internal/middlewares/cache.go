package middlewares

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	// 预编译正则表达式以提高性能
	staticFilePattern = regexp.MustCompile(`^/public|^/favicon\.ico`)
	jsFilePattern     = regexp.MustCompile(`\.js$`)
)

// CacheMiddleware 根据请求路径添加适当的缓存头
func CacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.RequestURI

		// JavaScript 文件不缓存
		if jsFilePattern.MatchString(path) {
			setNoCacheHeaders(c)
		} else if staticFilePattern.MatchString(path) {
			// 静态内容缓存 24 小时
			setCacheHeaders(c, time.Hour*24)
		} else if strings.HasPrefix(path, "/admin") {
			// 不缓存管理页面
			setNoCacheHeaders(c)
		}

		c.Next()
	}
}

// setCacheHeaders 设置带有指定持续时间的缓存头
func setCacheHeaders(c *gin.Context, duration time.Duration) {
	seconds := int(duration.Seconds())
	c.Header("Cache-Control", "public, max-age="+string(rune(seconds)))
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Type", getContentType(c.Request.URL.Path))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Expires", time.Now().Add(duration).UTC().Format(http.TimeFormat))
}

// setNoCacheHeaders 设置防止缓存的头信息
func setNoCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// getContentType 根据文件扩展名确定内容类型
func getContentType(path string) string {
	// 默认为 octet-stream
	contentType := "application/octet-stream"

	// 检查文件扩展名
	if strings.HasSuffix(path, ".css") {
		contentType = "text/css"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript"
	} else if strings.HasSuffix(path, ".html") {
		contentType = "text/html"
	} else if strings.HasSuffix(path, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(path, ".ico") {
		contentType = "image/x-icon"
	}

	return contentType
}
