package services

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/robfig/cron/v3"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"uranus/internal/models"
)

func GetCertificateInfo(domain string) *x509.Certificate {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	response, err := client.Get("https://" + domain)
	if err != nil {
		log.Printf("证书获取失败: %v", domain)
		return nil
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)
	return response.TLS.PeerCertificates[0]
}

func RenewSSL() {
	// 每天 00:05 进行检测
	spec := "5 0 * * *"
	c := cron.New()
	_, _ = c.AddFunc(spec, func() {
		for _, cert := range models.GetCertificates() {
			var need2Renew = false
			if cert.NotAfter.Unix() != -62135596800 {
				if cert.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
					log.Printf("%v => 需要续期\n", cert.Domains)
					need2Renew = true
				} else {
					log.Printf("%v => 证书续期时间: %v\n", cert.Domains, cert.NotAfter.Format("2006-01-02 15:04:05"))
				}
			}
			if need2Renew {
				_ = IssueCert(strings.Split(cert.Domains, ","), cert.FileName)
				ReloadNginx()
			}
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
