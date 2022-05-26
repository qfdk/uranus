package controllers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func Terminal(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "terminal.html", gin.H{})
}
