package controllers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"proxy-manager/app/tools"
	"strings"
)

func GetCertificate(ctx *gin.Context) {
	filename, _ := ctx.GetQuery("filename")
	var message string
	domain := strings.Split(filename, ".conf")[0]
	fmt.Println(domain)
	err := tools.IssueCert(domain)
	if err != nil {
		message = err.Error()
	} else {
		message = "OK"
	}
	ctx.JSON(http.StatusOK, gin.H{"message": message})
}
