package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func SSLDirs(ctx *gin.Context) {
	paths, err := ioutil.ReadDir(config.GetAppConfig().SSLPath)

	var result = make(gin.H)
	if err != nil {
		ctx.HTML(http.StatusOK, "ssl.html", gin.H{"files": []string{}})
		return
	}

	for _, _path := range paths {
		data, _ := ioutil.ReadFile(path.Join(config.GetAppConfig().SSLPath, _path.Name(), "domains"))
		result[_path.Name()] = strings.Split(string(data), ",")
	}
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{"files": result})
}

func IssueCert(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	var message string
	err := services.IssueCert(domains, configName)
	if err != nil {
		message = err.Error()
	} else {
		message = "OK"
	}
	ctx.JSON(http.StatusOK, gin.H{"message": message})
}

func CertInfo(ctx *gin.Context) {
	domain := ctx.Query("domain")
	certInfo := services.GetCertificateInfo(domain)
	ctx.JSON(http.StatusOK, gin.H{
		"domain":    certInfo.Subject.CommonName,
		"issuer":    certInfo.Issuer.CommonName,
		"not_after": certInfo.NotAfter,
	})
}

func DeleteSSL(ctx *gin.Context) {
	configName := ctx.Query("configName")
	os.RemoveAll(filepath.Join(config.GetAppConfig().SSLPath, configName))
	ctx.Redirect(http.StatusFound, "/ssl")
}
