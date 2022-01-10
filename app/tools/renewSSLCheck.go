package tools

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/robfig/cron/v3"
	"io/ioutil"
	"log"
	"net/http"
	"proxy-manager/app/services"
	"proxy-manager/config"
	"time"
)

func GetCertificateInfo(domain string) *x509.Certificate {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	response, err := client.Get("https://" + domain)
	if err != nil {
		log.Println(err.Error())
		return nil
	}
	defer response.Body.Close()

	certInfo := response.TLS.PeerCertificates[0]
	return certInfo
}

func RenewSSL() {
	// 每天 00:05 进行检测
	spec := "0 5 0 * * ? *"
	c := cron.New(cron.WithSeconds())
	c.AddFunc(spec, func() {
		sslPath := config.GetAppConfig().SSLPath
		files, _ := ioutil.ReadDir(sslPath)
		for _, file := range files {
			domain := file.Name()
			certInfo := GetCertificateInfo(domain)
			if certInfo != nil {
				if certInfo.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
					log.Println("证书过期，需要续签！")
					IssueCert(domain)
					services.ReloadNginx()
				} else {
					log.Printf("%s => 证书OK.\n", domain)
				}
			}
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
