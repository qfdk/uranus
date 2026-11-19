package services

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/robfig/cron/v3"
	"log"
	"net"
	"path/filepath"
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

// certsToRenew returns the certificates that expire within 30 days. Those of
// disabled sites are skipped: nginx no longer serves the site, so the HTTP
// challenge would fail.
func certsToRenew(certs []models.Cert, now time.Time) []models.Cert {
	disabled := map[string]bool{}
	for _, site := range models.DisabledSites() {
		disabled[strings.TrimSuffix(filepath.Base(site.Path), ".conf")] = true
	}
	var due []models.Cert
	for _, cert := range certs {
		switch {
		case cert.NotAfter.IsZero():
		case cert.NotAfter.Sub(now) >= 30*24*time.Hour:
			log.Printf("%v => 证书续期时间: %v\n", cert.Domains, cert.NotAfter.Format("2006-01-02 15:04:05"))
		case disabled[cert.FileName]:
			log.Printf("%v => 网站已停用，不续签\n", cert.Domains)
		default:
			log.Printf("%v => 需要续期\n", cert.Domains)
			due = append(due, cert)
		}
	}
	return due
}

func RenewSSL() {
	// 每天 00:05 进行检测
	spec := "5 0 * * *"
	c := cron.New()
	_, _ = c.AddFunc(spec, func() {
		for _, cert := range certsToRenew(models.GetCertificates(), time.Now()) {
			if err := IssueCert(strings.Split(cert.Domains, ","), cert.FileName); err != nil {
				log.Printf("%v => 证书续期失败: %v\n", cert.Domains, err)
				continue
			}
			ReloadNginx()
		}
	})
	go c.Start()
	defer c.Stop()
	select {}
}
