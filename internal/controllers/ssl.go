package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"uranus/internal/config"
	models2 "uranus/internal/models"
	services2 "uranus/internal/services"
)

func Certificates(ctx *gin.Context) {
	var results []gin.H
	for _, cert := range models2.GetCertificates() {
		if cert.NotAfter.Unix() != -62135596800 {
			results = append(results, gin.H{
				"configName": cert.FileName,
				"domains":    strings.Split(cert.Domains, ","),
				"expiredAt":  cert.NotAfter.Format("2006-01-02"),
			})
		}
	}
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{"results": results})
}

func IssueCert(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	message := "OK"
	err := services2.IssueCert(domains, configName)
	if err != nil {
		message = err.Error()
	}
	ctx.JSON(http.StatusOK, gin.H{"message": message})
}

func CertInfo(ctx *gin.Context) {
	domain := ctx.Query("domain")
	certInfo := services2.GetCertificateInfo(domain)
	ctx.JSON(http.StatusOK, gin.H{
		"domain":    certInfo.Subject.CommonName,
		"issuer":    certInfo.Issuer.CommonName,
		"not_after": certInfo.NotAfter,
	})
}

func DeleteSSL(ctx *gin.Context) {
	configName := ctx.Query("configName")
	cert := models2.GetCertByFilename(configName)
	models2.GetDbClient().Model(&cert).Select("not_after").Updates(map[string]interface{}{"not_after": gorm.Expr("NULL")})
	_ = os.RemoveAll(filepath.Join(config.GetAppConfig().SSLPath, configName))
	ctx.Redirect(http.StatusFound, "/admin/ssl")
}
