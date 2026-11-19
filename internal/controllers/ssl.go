package controllers

import (
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uranus/internal/config"
	models2 "uranus/internal/models"
	services2 "uranus/internal/services"
)

func Certificates(ctx *gin.Context) {
	var results []gin.H
	for _, cert := range models2.IssuedCertificates() {
		results = append(results, gin.H{
			"configName": cert.FileName,
			"domains":    strings.Split(cert.Domains, ","),
			"expiredAt":  cert.NotAfter.Format("2006-01-02"),
			"daysLeft":   int(time.Until(cert.NotAfter).Hours() / 24),
		})
	}
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{
		"activePage":  "ssl",
		"results":     results,
		"deleted":     ctx.Query("deleted"),
		"deleteError": ctx.Query("error"),
	})
}

func IssueCert(ctx *gin.Context) {
	domains := ctx.PostFormArray("domains[]")
	configName := ctx.PostForm("configName")
	message := "OK"
	err := services2.IssueCert(domains, configName)
	if err != nil {
		message = err.Error()
	}
	ctx.JSON(http.StatusOK, gin.H{"message": message})
}

func CertInfo(ctx *gin.Context) {
	domain := ctx.Query("domain")
	if domain == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "缺少 domain 参数"})
		return
	}
	certInfo, err := services2.GetCertificateInfo(domain)
	if err != nil {
		ctx.JSON(http.StatusBadGateway, gin.H{"error": "证书获取失败: " + err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"domain":    certInfo.Subject.CommonName,
		"issuer":    certInfo.Issuer.CommonName,
		"not_after": certInfo.NotAfter,
	})
}

func DeleteSSL(ctx *gin.Context) {
	configName := ctx.PostForm("configName")

	// 验证 configName 防止路径遍历攻击
	if configName == "" || strings.Contains(configName, "..") || strings.Contains(configName, "/") || strings.Contains(configName, "\\") {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config name"})
		return
	}

	// 还有网站在用就不删：证书文件没了，下一次重载 nginx 会失败，所有网站都受影响
	if users := services2.CertificateUsers(configName); len(users) > 0 {
		message := "证书 " + configName + " 还在被下面的配置使用，先修改或删除这些网站：\n" + strings.Join(users, "\n")
		ctx.Redirect(http.StatusFound, "/admin/ssl?"+url.Values{"error": {message}}.Encode())
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
	baseSSLPath := filepath.Clean(config.SSLDir())
	sslPath := filepath.Clean(filepath.Join(baseSSLPath, configName))

	// 确保目标路径在 SSL 目录内
	if !strings.HasPrefix(sslPath, baseSSLPath) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid path"})
		return
	}

	if err := os.RemoveAll(sslPath); err != nil {
		log.Printf("Failed to remove SSL files: %v", err)
	}

	ctx.Redirect(http.StatusFound, "/admin/ssl?"+url.Values{"deleted": {configName}}.Encode())
}
