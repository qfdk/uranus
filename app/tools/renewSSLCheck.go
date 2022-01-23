package tools

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"github.com/robfig/cron/v3"
	"io/ioutil"
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
		panic(err)
		return nil
	}
	defer response.Body.Close()

	certInfo := response.TLS.PeerCertificates[0]
	return certInfo
}

func RenewSSL() {
	// 每天 00:05 进行检测
	spec := "5 0 * * *"
	c := cron.New()
	c.AddFunc(spec, func() {
		sslPath := config.GetAppConfig().SSLPath
		files, _ := ioutil.ReadDir(sslPath)
		for _, file := range files {
			domains := strings.Split(file.Name(), ",")
			var needRenew = false
			for _, domain := range domains {
				fmt.Printf("开始获取证书信息: %s\n", domain)
				certInfo := GetCertificateInfo(domain)
				if certInfo != nil {
					if certInfo.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
						fmt.Printf("%s 证书过期，需要续签！\n", domain)
						needRenew = true
					} else {
						fmt.Printf("%s => 证书OK.\n", domain)
					}
				}
			}
			if needRenew {
				IssueCert(domains)
				services.ReloadNginx()
			}
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
