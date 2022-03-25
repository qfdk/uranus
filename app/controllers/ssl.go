package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"nginx-proxy-manager/app/config"
	"nginx-proxy-manager/app/models"
	"nginx-proxy-manager/app/services"
	"os"
	"path/filepath"
	"strings"
)

func Certificates(ctx *gin.Context) {
	var results []gin.H
	for _, cert := range models.GetCertificates() {
		if cert.NotAfter.Unix() != -62135596800 {
			results = append(results, gin.H{
				"configName": cert.FileName,
				"domains":    strings.Split(cert.Domains, ","),
				"expiredAt":  cert.NotAfter.Format("2006-01-02 15:04:05"),
			})
		}
	}
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{"results": results})
}

func IssueCert(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	message := "OK"
	err := services.IssueCert(domains, configName)
	if err != nil {
		message = err.Error()
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
	cert := models.GetCertByFilename(configName)
	config.GetDbClient().Model(&cert).Select("not_after").Updates(map[string]interface{}{"not_after": gorm.Expr("NULL")})
	os.RemoveAll(filepath.Join(config.GetAppConfig().SSLPath, configName))
	ctx.Redirect(http.StatusFound, "/ssl")
}
