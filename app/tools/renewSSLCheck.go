package tools

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
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
	Content  string `json:"content"`
	Expired  int64  `json:"expired"`
	Domains  string `json:"domains"`
	FileName string `json:"fileName"`
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
			if output.Expired != 0 {
				if (output.Expired - time.Now().Unix()) < 0 {
					fmt.Printf("%v 证书过期，需要续签！\n", output.Domains)
					need2Renew = true
				} else {
					fmt.Printf("%v => 证书续期时间: %v\n", output.Domains, time.Unix(output.Expired, 0).Format("2006-01-02 15:04:05"))
				}
			}
			if need2Renew {
				IssueCert(strings.Split(output.Domains, ","), output.FileName)
				services.ReloadNginx()
			}
		}
		//sslPath := config.GetAppConfig().SSLPath
		//files, _ := ioutil.ReadDir(sslPath)
		//for _, file := range files {
		//	data, _ := ioutil.ReadFile(path.Join(config.GetAppConfig().SSLPath, file.Name(), "domains"))
		//	var domains = strings.Split(string(data), ",")
		//	var need2Renew = false
		//	for _, domain := range domains {
		//		fmt.Printf("开始获取证书信息: %s\n", domain)
		//		certInfo := GetCertificateInfo(domain)
		//		if certInfo != nil {
		//			if certInfo.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
		//				fmt.Printf("%s 证书过期，需要续签！\n", domain)
		//				need2Renew = true
		//			} else {
		//				fmt.Printf("%s => 证书OK.\n", domain)
		//			}
		//		}
		//	}
		//	if need2Renew {
		//		IssueCert(domains, file.Name())
		//		services.ReloadNginx()
		//	}
		//}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
