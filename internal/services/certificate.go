package services

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"github.com/gin-gonic/gin"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	. "uranus/internal/config"
	models2 "uranus/internal/models"
)

// MyUser You'll need a user or account type that implements acme.User
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

func IssueCert(domains []string, configName string) error {
	// Create a user. New accounts need an email and private key to start.
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	// 如果没有传入域名的话，默认是点击续签
	// 需要读取保存的domains 列表
	if len(domains) == 0 {
		data, err := os.ReadFile(path.Join(GetAppConfig().SSLPath, configName, "domains"))
		if err != nil {
			log.Printf("[SSL] Failed to read domains file: %v", err)
			return err
		}
		domains = strings.Split(string(data), ",")
	}

	myUser := MyUser{
		Email: GetAppConfig().Email,
		key:   privateKey,
	}

	config := lego.NewConfig(&myUser)

	// 不是生产环境用测试 URL
	if gin.Mode() != gin.ReleaseMode {
		config.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	config.Certificate.KeyType = certcrypto.RSA2048

	// A client facilitates communication with the CA server.
	client, err := lego.NewClient(config)
	if err != nil {
		log.Printf("[SSL] Failed to create ACME client: %v", err)
		return err
	}

	// 9999 端口为签名端口
	err = client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "9999"))
	if err != nil {
		return err
	}

	// New users will need to register
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return err
	}
	myUser.Registration = reg

	certificates, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})

	if err != nil {
		return err
	}

	pCert, err := certcrypto.ParsePEMCertificate(certificates.Certificate)
	if err != nil {
		return err
	}

	// nginx 证书目录
	if err := saveCertificate(filepath.Join(GetAppConfig().SSLPath, configName),
		certificates.Certificate, certificates.PrivateKey, domains); err != nil {
		return err
	}

	cert := models2.GetCertByFilename(configName)
	cert.NotAfter = pCert.NotAfter
	if err := models2.GetDbClient().Save(&cert).Error; err != nil {
		return err
	}

	log.Printf("[+] SSL任务完成, 证书到期时间 : %v\n", pCert.NotAfter.Format("2006-01-02 15:04:05"))
	return nil
}

// saveCertificate writes the certificate chain, its private key and the domain
// list where the site configuration expects them. The private key is readable
// by its owner only: nginx loads it as root before dropping privileges.
func saveCertificate(dir string, chain, privateKey []byte, domains []string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
		perm os.FileMode
	}{
		{"fullchain.cer", chain, 0644},
		{"private.key", privateKey, 0600},
		{"domains", []byte(strings.Join(domains, ",")), 0644},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, f.data, f.perm); err != nil {
			return err
		}
		// WriteFile keeps the mode of an existing file; renewals must fix old 0644 keys too.
		if err := os.Chmod(path, f.perm); err != nil {
			return err
		}
	}
	return nil
}
