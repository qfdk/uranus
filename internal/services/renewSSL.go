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
	"uranus/internal/models"

	"github.com/robfig/cron/v3"
)

var (
	// Cache for certificate information to avoid repeated TLS handshakes
	certInfoCache     = make(map[string]*x509.Certificate)
	certInfoCacheLock sync.RWMutex
	certInfoExpiry    = make(map[string]time.Time)
)

// GetCertificateInfo retrieves certificate info with caching
func GetCertificateInfo(domain string) *x509.Certificate {
	// Check cache first (read lock)
	certInfoCacheLock.RLock()
	if cert, ok := certInfoCache[domain]; ok {
		if time.Now().Before(certInfoExpiry[domain]) {
			certInfoCacheLock.RUnlock()
			return cert
		}
	}
	certInfoCacheLock.RUnlock()

	// Cache miss or expired, fetch new certificate
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		// Performance tuning
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second, // Prevent hanging
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

	// Update cache (write lock)
	certInfoCacheLock.Lock()
	certInfoCache[domain] = cert
	certInfoExpiry[domain] = time.Now().Add(1 * time.Hour) // Cache for 1 hour
	certInfoCacheLock.Unlock()

	return cert
}

// RenewSSLWithContext runs SSL renewal with context support for graceful shutdown
func RenewSSLWithContext(ctx context.Context) {
	// Every day at 00:05 check certificates
	spec := "5 0 * * *"
	c := cron.New()

	_, _ = c.AddFunc(spec, func() {
		log.Println("[SSL] Starting certificate renewal check")

		// Process certificates in parallel with a limit
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 5) // Limit to 5 concurrent operations

		certs := models.GetCertificates()
		for _, cert := range certs {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				log.Println("[SSL] Certificate renewal cancelled by context")
				return
			default:
				// Continue processing
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
				semaphore <- struct{}{} // Acquire semaphore to limit concurrency

				go func(cert models.Cert) {
					defer wg.Done()
					defer func() { <-semaphore }() // Release semaphore

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

	// Wait for context cancellation to stop the cron scheduler
	<-ctx.Done()
	log.Println("[SSL] Stopping certificate renewal service")
	c.Stop()
}

// RenewSSL for backward compatibility
func RenewSSL() {
	// Use background context that will never be canceled
	RenewSSLWithContext(context.Background())
}
