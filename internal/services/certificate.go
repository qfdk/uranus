package services

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"log"

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

// Cache for private keys to avoid regenerating them frequently
var (
	privateKeyCache     crypto.PrivateKey
	privateKeyCacheLock sync.RWMutex
	clientCache         map[string]*lego.Client
	clientCacheLock     sync.RWMutex
)

func init() {
	clientCache = make(map[string]*lego.Client)
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

	// Generate a new key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	// Update cache
	privateKeyCacheLock.Lock()
	privateKeyCache = privateKey
	privateKeyCacheLock.Unlock()

	return privateKey, nil
}

// getClient returns a cached ACME client or creates a new one
func getClient(email string, key crypto.PrivateKey) (*lego.Client, error) {
	cacheKey := email

	// Check cache
	clientCacheLock.RLock()
	if client, ok := clientCache[cacheKey]; ok {
		clientCacheLock.RUnlock()
		return client, nil
	}
	clientCacheLock.RUnlock()

	// Create new client
	myUser := MyUser{
		Email: email,
		key:   key,
	}

	config := lego.NewConfig(&myUser)

	// Use test URL in non-production environments
	if gin.Mode() != gin.ReleaseMode {
		config.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	config.Certificate.KeyType = certcrypto.RSA2048

	// Create client
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACME client: %w", err)
	}

	// Port 9999 is for signing
	err = client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "9999"))
	if err != nil {
		return nil, fmt.Errorf("failed to set HTTP challenge provider: %w", err)
	}

	// Register the user
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("failed to register with ACME: %w", err)
	}
	myUser.Registration = reg

	// Update cache
	clientCacheLock.Lock()
	clientCache[cacheKey] = client
	clientCacheLock.Unlock()

	return client, nil
}

func IssueCert(domains []string, configName string) error {
	// Get or create a private key
	privateKey, err := getOrCreatePrivateKey()
	if err != nil {
		return fmt.Errorf("failed to create private key: %w", err)
	}

	// If no domains are provided, default is to renew
	// Need to read saved domains list
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

	// Get client
	client, err := getClient(GetAppConfig().Email, privateKey)
	if err != nil {
		return err
	}

	// Request certificate
	certificates, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})

	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// nginx certificate directory
	certificateSavedDir := filepath.Join(GetAppConfig().SSLPath, configName)
	if _, err := os.Stat(certificateSavedDir); os.IsNotExist(err) {
		err = os.MkdirAll(certificateSavedDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create certificate directory: %w", err)
		}
	}

	// Save certificate
	err = os.WriteFile(filepath.Join(certificateSavedDir, "fullchain.cer"),
		certificates.Certificate, 0644)
	if err != nil {
		return fmt.Errorf("failed to write certificate file: %w", err)
	}

	// Save private key
	err = os.WriteFile(filepath.Join(certificateSavedDir, "private.key"),
		certificates.PrivateKey, 0644)
	if err != nil {
		return fmt.Errorf("failed to write private key file: %w", err)
	}

	// Save domains
	err = os.WriteFile(filepath.Join(certificateSavedDir, "domains"),
		[]byte(strings.Join(domains, ",")), 0644)
	if err != nil {
		return fmt.Errorf("failed to write domains file: %w", err)
	}

	pCert, err := certcrypto.ParsePEMCertificate(certificates.Certificate)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Update database
	cert := models.GetCertByFilename(configName)
	cert.NotAfter = pCert.NotAfter
	err = models.GetDbClient().Save(&cert).Error
	if err != nil {
		return fmt.Errorf("failed to save certificate to database: %w", err)
	}

	log.Printf("[+] SSL task completed, certificate expires on: %v\n", pCert.NotAfter.Format("2006-01-02 15:04:05"))

	// Invalidate certificate cache
	certInfoCacheLock.Lock()
	for _, domain := range domains {
		delete(certInfoCache, domain)
		delete(certInfoExpiry, domain)
	}
	certInfoCacheLock.Unlock()

	return nil
}
