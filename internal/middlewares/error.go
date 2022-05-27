package middlewares

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func ErrorHttp(c *gin.Context) {
	defer func(c *gin.Context) {
		if rec := recover(); rec != nil {
			println("panic: %v", rec)
			c.HTML(http.StatusOK, "error", gin.H{"message": rec})
		}
	}(c)
	c.Next()
}
