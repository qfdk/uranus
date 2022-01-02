package middlewares

import (
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

func ErrorHttp(c *gin.Context) {
	defer func(c *gin.Context) {
		if rec := recover(); rec != nil {
			log.Printf("panic: %v\n", rec)
			c.HTML(http.StatusOK, "error", gin.H{"message": rec})
		}
	}(c)
	c.Next()
}
