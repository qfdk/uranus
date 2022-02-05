package services

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/robfig/cron/v3"
	"net/http"
	"strings"
	"time"
)

func GetCertificateInfo(domain string) *x509.Certificate {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	response, err := client.Get("https://" + domain)
	if err != nil {
		fmt.Sprintf("证书获取失败: %v", domain)
		return nil
	}
	defer response.Body.Close()
	return response.TLS.PeerCertificates[0]
}

type RedisData struct {
	Content  string    `json:"content"`
	NotAfter time.Time `json:"notAfter"`
	Domains  string    `json:"domains"`
	FileName string    `json:"fileName"`
	Proxy    string    `json:"proxy"`
}

func RenewSSL() {
	// 每天 00:05 进行检测
	spec := "5 0 * * *"
	c := cron.New()
	c.AddFunc(spec, func() {
		keys := config.RedisKeys()
		for _, key := range keys {
			redisData := config.RedisGet(strings.Split(key, ":")[1])
			var need2Renew = false
			var output RedisData
			json.Unmarshal([]byte(redisData), &output)
			if output.NotAfter.Unix() != -62135596800 {
				if output.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
					fmt.Printf("%v => 需要续期\n", output.Domains)
					need2Renew = true
				} else {
					fmt.Printf("%v => 证书续期时间: %v\n", output.Domains, output.NotAfter.Format("2006-01-02 15:04:05"))
				}
			}
			if need2Renew {
				IssueCert(strings.Split(output.Domains, ","), output.FileName)
				ReloadNginx()
			}
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
