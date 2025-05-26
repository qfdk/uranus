package controllers

import (
	"github.com/gin-gonic/gin"
	"log"
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
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{
		"activePage": "ssl",
		"results":    results,
	})
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

	// 验证 configName 防止路径遍历攻击
	if configName == "" || strings.Contains(configName, "..") || strings.Contains(configName, "/") || strings.Contains(configName, "\\") {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config name"})
		return
	}

	cert := models2.GetCertByFilename(configName)
	if cert.ID == 0 {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Certificate not found"})
		return
	}

	// 使用 NULL 更新 not_after 字段
	if err := models2.GetDbClient().Model(&cert).Update("not_after", nil).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update certificate"})
		return
	}

	// 安全地删除文件
	sslPath := filepath.Clean(filepath.Join(config.GetAppConfig().SSLPath, configName))
	baseSSLPath := filepath.Clean(config.GetAppConfig().SSLPath)

	// 确保目标路径在 SSL 目录内
	if !strings.HasPrefix(sslPath, baseSSLPath) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid path"})
		return
	}

	if err := os.RemoveAll(sslPath); err != nil {
		log.Printf("Failed to remove SSL files: %v", err)
	}

	ctx.Redirect(http.StatusFound, "/admin/ssl")
}
