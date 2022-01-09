package tools

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	pmConfig "proxy-manager/config"
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

func IssueCert(domain string) error {
	// Create a user. New accounts need an email and private key to start.
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}

	myUser := MyUser{
		Email: pmConfig.GetAppConfig().Email,
		key:   privateKey,
	}

	config := lego.NewConfig(&myUser)

	//config.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	//config.CADirURL = "http://127.0.0.1:4001/directory"
	config.Certificate.KeyType = certcrypto.RSA2048

	// A client facilitates communication with the CA server.
	client, err := lego.NewClient(config)
	if err != nil {
		log.Println(err)
		return err
	}

	// 9999 端口为签名端口
	err = client.Challenge.SetHTTP01Provider(http01.NewProviderServer("", "9999"))
	if err != nil {
		log.Println(err)
		return err
	}

	// New users will need to register
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		log.Println(err)
		return err
	}
	myUser.Registration = reg

	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		log.Println(err)
		return err
	}
	// nginx 根目录
	saveDir := filepath.Join(pmConfig.GetAppConfig().SSLPath, domain)
	if _, err := os.Stat(saveDir); os.IsNotExist(err) {
		err = os.MkdirAll(saveDir, 0755)
		if err != nil {
			fmt.Println(err)
			log.Println("无法建立文件夹", saveDir)
			return err
		}
	}

	// Each certificate comes back with the cert bytes, the bytes of the client's
	// private key, and a certificate URL. SAVE THESE TO DISK.
	err = ioutil.WriteFile(filepath.Join(saveDir, "fullchain.cer"),
		certificates.Certificate, 0644)
	if err != nil {
		log.Println(err)
		return err
	}
	err = ioutil.WriteFile(filepath.Join(saveDir, "private.key"),
		certificates.PrivateKey, 0644)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}
