package controllers

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

// WebTerminalPage serves the WebSocket-based terminal page
func WebTerminalPage(c *gin.Context) {
	c.HTML(http.StatusOK, "terminal.html", gin.H{
		"title": "Terminal",
	})
}