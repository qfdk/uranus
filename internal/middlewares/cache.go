package middlewares

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	// Precompiled regex patterns for better performance
	staticFilePattern = regexp.MustCompile(`^/public|^/favicon\.ico`)
)

// CacheMiddleware adds appropriate cache headers based on the request path
func CacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.RequestURI

		// Fast path checking with regex
		if staticFilePattern.MatchString(path) {
			setCacheHeaders(c, time.Hour*24) // Cache static content for 24 hours
		} else if strings.HasPrefix(path, "/admin") {
			// Don't cache admin pages
			setNoCacheHeaders(c)
		}

		c.Next()
	}
}

// setCacheHeaders sets appropriate cache headers with the given duration
func setCacheHeaders(c *gin.Context, duration time.Duration) {
	seconds := int(duration.Seconds())
	c.Header("Cache-Control", "public, max-age="+string(rune(seconds)))
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Type", getContentType(c.Request.URL.Path))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Expires", time.Now().Add(duration).UTC().Format(http.TimeFormat))
}

// setNoCacheHeaders sets headers to prevent caching
func setNoCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// getContentType determines the content type based on file extension
func getContentType(path string) string {
	// Default to octet-stream
	contentType := "application/octet-stream"

	// Check file extension
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
