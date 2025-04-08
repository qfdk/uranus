package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"uranus/internal/models"
)

// 缓存变量
var (
	// 证书信息缓存
	certInfoCache     = make(map[string]*x509.Certificate)
	certInfoCacheLock sync.RWMutex
	certInfoExpiry    = make(map[string]time.Time)
)

// GetCertificateInfo retrieves certificate info with caching
func GetCertificateInfo(domain string) *x509.Certificate {
	// 首先检查缓存（读锁）
	certInfoCacheLock.RLock()
	if cert, ok := certInfoCache[domain]; ok {
		if time.Now().Before(certInfoExpiry[domain]) {
			certInfoCacheLock.RUnlock()
			return cert
		}
	}
	certInfoCacheLock.RUnlock()

	// 缓存未命中或已过期，获取新证书
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second, // 防止挂起
	}

	response, err := client.Get("https://" + domain)
	if err != nil {
		log.Printf("Certificate retrieval failed for %s: %v", domain, err)
		return nil
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	cert := response.TLS.PeerCertificates[0]

	// 更新缓存（写锁）
	certInfoCacheLock.Lock()
	certInfoCache[domain] = cert
	certInfoExpiry[domain] = time.Now().Add(1 * time.Hour) // 缓存 1 小时
	certInfoCacheLock.Unlock()

	return cert
}

// RenewSSLWithContext runs SSL renewal with context support for graceful shutdown
func RenewSSLWithContext(ctx context.Context) {
	// 每天凌晨 00:05 检查证书
	spec := "5 0 * * *"
	c := cron.New()

	_, _ = c.AddFunc(spec, func() {
		log.Println("[SSL] Starting certificate renewal check")

		// 并行处理证书，限制并发数
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 5) // 限制为 5 个并发操作

		certs := models.GetCertificates()
		for _, cert := range certs {
			// 检查上下文是否被取消
			select {
			case <-ctx.Done():
				log.Println("[SSL] Certificate renewal cancelled by context")
				return
			default:
				// 继续处理
			}

			var need2Renew = false
			if cert.NotAfter.Unix() != -62135596800 {
				if cert.NotAfter.Sub(time.Now()) < time.Hour*24*30 {
					log.Printf("%v => Needs renewal", cert.Domains)
					need2Renew = true
				} else {
					log.Printf("%v => Certificate expires on: %v",
						cert.Domains,
						cert.NotAfter.Format("2006-01-02 15:04:05"))
				}
			}

			if need2Renew {
				wg.Add(1)
				semaphore <- struct{}{} // 获取信号量以限制并发

				go func(cert models.Cert) {
					defer wg.Done()
					defer func() { <-semaphore }() // 释放信号量

					err := IssueCert(strings.Split(cert.Domains, ","), cert.FileName)
					if err != nil {
						log.Printf("[SSL] Error renewing certificate for %s: %v", cert.Domains, err)
					} else {
						log.Printf("[SSL] Successfully renewed certificate for %s", cert.Domains)
						ReloadNginx()
					}
				}(cert)
			}
		}

		wg.Wait()
		log.Println("[SSL] Certificate renewal check completed")
	})

	go c.Start()

	// 等待上下文取消以停止 cron 调度器
	<-ctx.Done()
	log.Println("[SSL] Stopping certificate renewal service")
	c.Stop()
}

// RenewSSL for backward compatibility
func RenewSSL() {
	// 使用永不取消的后台上下文
	RenewSSLWithContext(context.Background())
}
