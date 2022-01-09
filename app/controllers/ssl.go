package controllers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"proxy-manager/app/tools"
)

func IssueCert(ctx *gin.Context) {
	domain, _ := ctx.GetQuery("domain")
	var message string
	err := tools.IssueCert(domain)
	if err != nil {
		message = err.Error()
	} else {
		message = "OK"
	}
	ctx.JSON(http.StatusOK, gin.H{"message": message})
}
