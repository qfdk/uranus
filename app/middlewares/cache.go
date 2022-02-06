package middlewares

import (
	"github.com/gin-gonic/gin"
	"strings"
)

// NoCache is a middleware function that appends headers
// to prevent the client from caching the HTTP response.
func CacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.RequestURI, "/public/icon") || strings.HasPrefix(c.Request.RequestURI, "favicon.ico") {
			c.Header("Cache-Control", "max-age=86400")
			c.Header("Content-Description", "File Transfer")
			c.Header("Content-Type", "application/octet-stream")
			c.Header("Content-Transfer-Encoding", "binary")
		}
	}
}
