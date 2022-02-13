package controllers

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func Certificates(ctx *gin.Context) {
	keys := config.RedisClient.Keys(config.RedisPrefix + "*")
	var results []gin.H
	for _, key := range keys.Val() {
		if !strings.Contains(key, "default") {
			var output config.RedisData
			redisResult, _ := config.RedisClient.Get(key).Result()
			json.Unmarshal([]byte(redisResult), &output)
			if output.NotAfter.Unix() != -62135596800 {
				results = append(results, gin.H{
					"configName": output.FileName,
					"domains":    strings.Split(output.Domains, ","),
					"expiredAt":  output.NotAfter.Format("2006-01-02 15:04:05"),
				})
			}
		}
	}
	ctx.HTML(http.StatusOK, "ssl.html", gin.H{"results": results})
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
	config.RedisClient.Del(config.RedisPrefix + configName)
	ctx.Redirect(http.StatusFound, "/ssl")
}
