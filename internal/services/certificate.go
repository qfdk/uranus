package services

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	. "uranus/internal/config"
	"uranus/internal/models"
)

// MyUser implements the acme.User interface
type MyUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *MyUser) GetEmail() string {
	return u.Email
}
func (u MyUser) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *MyUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

// 缓存变量
var (
	// 缓存私钥以避免频繁重新生成
	privateKeyCache     crypto.PrivateKey
	privateKeyCacheLock sync.RWMutex

	// 缓存 ACME 客户端
	clientCache     map[string]*lego.Client
	clientCacheLock sync.RWMutex

	// 证书信息缓存
	certInfoCache     = make(map[string]*x509.Certificate)
	certInfoCacheLock sync.RWMutex
	certInfoExpiry    = make(map[string]time.Time)
)

func init() {
	clientCache = make(map[string]*lego.Client)
}

// HTTP Challenge 服务管理
type ChallengeServerManager struct {
	server     *http.Server
	provider   *http01.HTTPChallengeProvider
	started    bool
	startMutex sync.Mutex
}

// 全局 Challenge 服务管理器
var challengeServerManager = &ChallengeServerManager{}

// 启动 HTTP Challenge 服务
func (m *ChallengeServerManager) Start() error {
	m.startMutex.Lock()
	defer m.startMutex.Unlock()

	if m.started {
		return nil
	}

	// 创建 HTTP Challenge 提供器
	provider := http01.NewProviderServer("", "9999")
	
	// 启动服务器
	srv := &http.Server{
		Addr:              ":9999",
		Handler:           provider.HTTPChallengeHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", ":9999")
	if err != nil {
		return fmt.Errorf("failed to listen on port 9999: %w", err)
	}

	go func() {
		log.Println("[SSL] HTTP Challenge server starting on port 9999")
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[SSL] Challenge server error: %v", err)
		}
	}()

	m.server = srv
	m.provider = provider
	m.started = true

	return nil
}

// 关闭 HTTP Challenge 服务
func (m *ChallengeServerManager) Stop() {
	m.startMutex.Lock()
	defer m.startMutex.Unlock()

	if !m.started {
		return
	}

	log.Println("[SSL] Stopping HTTP Challenge server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if m.server != nil {
		if err := m.server.Shutdown(ctx); err != nil {
			log.Printf("[SSL] Error stopping challenge server: %v", err)
		}
	}

	m.server = nil
	m.provider = nil
	m.started = false
}

// getOrCreatePrivateKey returns a cached private key or generates a new one
func getOrCreatePrivateKey() (crypto.PrivateKey, error) {
	privateKeyCacheLock.RLock()
	if privateKeyCache != nil {
		key := privateKeyCache
		privateKeyCacheLock.RUnlock()
		return key, nil
	}
	privateKeyCacheLock.RUnlock()

	// 生成新密钥
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	privateKeyCacheLock.Lock()
	privateKeyCache = privateKey
	privateKeyCacheLock.Unlock()

	return privateKey, nil
}

// getClient returns a cached ACME client or creates a new one
func getClient(email string, key crypto.PrivateKey) (*lego.Client, error) {
	cacheKey := email

	// 检查缓存
	clientCacheLock.RLock()
	if client, ok := clientCache[cacheKey]; ok {
		clientCacheLock.RUnlock()
		return client, nil
	}
	clientCacheLock.RUnlock()

	// 创建新客户端
	myUser := MyUser{
		Email: email,
		key:   key,
	}

	config := lego.NewConfig(&myUser)

	// 非生产环境使用测试 URL
	if gin.Mode() != gin.ReleaseMode {
		config.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	config.Certificate.KeyType = certcrypto.RSA2048

	// 创建客户端
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACME client: %w", err)
	}

	// 注册用户
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("failed to register with ACME: %w", err)
	}
	myUser.Registration = reg

	// 更新缓存
	clientCacheLock.Lock()
	clientCache[cacheKey] = client
	clientCacheLock.Unlock()

	return client, nil
}

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

func IssueCert(domains []string, configName string) error {
	// 启动 HTTP Challenge 服务
	if err := challengeServerManager.Start(); err != nil {
		return fmt.Errorf("failed to start challenge server: %w", err)
	}
	// 确保在函数结束时关闭服务
	defer challengeServerManager.Stop()

	// 获取或创建私钥
	privateKey, err := getOrCreatePrivateKey()
	if err != nil {
		return fmt.Errorf("failed to create private key: %w", err)
	}

	// 如果没有提供域名，尝试从文件读取
	if len(domains) == 0 {
		domainsFilePath := path.Join(GetAppConfig().SSLPath, configName, "domains")
		if _, err := os.Stat(domainsFilePath); os.IsNotExist(err) {
			return fmt.Errorf("domains file not found for config %s", configName)
		}

		data, err := os.ReadFile(domainsFilePath)
		if err != nil {
			return fmt.Errorf("failed to read domains file: %w", err)
		}
		domains = strings.Split(string(data), ",")
	}

	// 获取 ACME 客户端
	client, err := getClient(GetAppConfig().Email, privateKey)
	if err != nil {
		return err
	}

	// 设置 HTTP Challenge 提供器
	err = client.Challenge.SetHTTP01Provider(challengeServerManager.provider)
	if err != nil {
		return fmt.Errorf("failed to set HTTP challenge provider: %w", err)
	}

	// 请求证书
	certificates, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})

	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// nginx 证书目录
	certificateSavedDir := filepath.Join(GetAppConfig().SSLPath, configName)
	if _, err := os.Stat(certificateSavedDir); os.IsNotExist(err) {
		err = os.MkdirAll(certificateSavedDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create certificate directory: %w", err)
		}
	}

	// 保存证书
	err = os.WriteFile(filepath.Join(certificateSavedDir, "fullchain.cer"),
		certificates.Certificate, 0644)
	if err != nil {
		return fmt.Errorf("failed to write certificate file: %w", err)
	}

	// 保存私钥
	err = os.WriteFile(filepath.Join(certificateSavedDir, "private.key"),
		certificates.PrivateKey, 0644)
	if err != nil {
		return fmt.Errorf("failed to write private key file: %w", err)
	}

	// 保存域名列表
	err = os.WriteFile(filepath.Join(certificateSavedDir, "domains"),
		[]byte(strings.Join(domains, ",")), 0644)
	if err != nil {
		return fmt.Errorf("failed to write domains file: %w", err)
	}

	pCert, err := certcrypto.ParsePEMCertificate(certificates.Certificate)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// 更新数据库
	cert := models.GetCertByFilename(configName)
	cert.NotAfter = pCert.NotAfter
	err = models.GetDbClient().Save(&cert).Error
	if err != nil {
		return fmt.Errorf("failed to save certificate to database: %w", err)
	}

	log.Printf("[+] SSL task completed, certificate expires on: %v\n", pCert.NotAfter.Format("2006-01-02 15:04:05"))

	// 使证书缓存失效
	certInfoCacheLock.Lock()
	for _, domain := range domains {
		delete(certInfoCache, domain)
		delete(certInfoExpiry, domain)
	}
	certInfoCacheLock.Unlock()

	return nil
}
