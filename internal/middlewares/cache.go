package middlewares

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"regexp"
	"strconv"
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

		// 第三方库的目录名带版本号，换版本就换路径，可以一直缓存
		if strings.HasPrefix(path, "/public/vendor/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else if jsFilePattern.MatchString(path) {
			// 自己的 JavaScript 文件不缓存
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

// setCacheHeaders 设置带有指定持续时间的缓存头。Content-Type 由返回文件的处理函数设置。
func setCacheHeaders(c *gin.Context, duration time.Duration) {
	c.Header("Cache-Control", "public, max-age="+strconv.Itoa(int(duration.Seconds())))
	c.Header("Expires", time.Now().Add(duration).UTC().Format(http.TimeFormat))
}

// setNoCacheHeaders 设置防止缓存的头信息
func setNoCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}
