package services

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"uranus/internal/models"
)

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
