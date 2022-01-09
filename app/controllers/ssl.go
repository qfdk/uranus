package controllers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"proxy-manager/app/tools"
	"proxy-manager/config"
)

func SSLDirs(ctx *gin.Context) {
	files, err := ioutil.ReadDir(config.GetAppConfig().SSLPath)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "ssl", gin.H{"files": files})
}

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

func CertInfo(ctx *gin.Context) {
	domain := ctx.Query("domain")
	certInfo := tools.GetCertificateInfo(domain)
	ctx.JSON(http.StatusOK, gin.H{
		"domain":    certInfo.Subject.CommonName,
		"issuer":    certInfo.Issuer.CommonName,
		"not_after": certInfo.NotAfter,
	})
}

func DeleteSSL(ctx *gin.Context) {
	domain := ctx.Query("domain")
	path := filepath.Join(config.GetAppConfig().SSLPath, domain)
	os.RemoveAll(path)
	ctx.Redirect(http.StatusFound, "/ssl")
}
