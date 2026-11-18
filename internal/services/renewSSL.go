package services

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/robfig/cron/v3"
	"log"
	"net"
	"strings"
	"time"
	"uranus/internal/models"
)

// GetCertificateInfo returns the certificate served for domain (port 443
// unless given as host:port). It connects directly instead of making an HTTP
// request, so a redirect to another host can't return that host's certificate.
func GetCertificateInfo(domain string) (*x509.Certificate, error) {
	host, port, err := net.SplitHostPort(domain)
	if err != nil {
		host, port = domain, "443"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), &tls.Config{
		ServerName: host,
		// 需要查看任何证书，包括已过期或自签名的
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("证书获取失败: %v: %v", domain, err)
		return nil, err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("服务器没有提供证书")
	}
	return certs[0], nil
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
				if err := IssueCert(strings.Split(cert.Domains, ","), cert.FileName); err != nil {
					log.Printf("%v => 证书续期失败: %v\n", cert.Domains, err)
					continue
				}
				ReloadNginx()
			}
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
