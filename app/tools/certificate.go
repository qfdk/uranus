package tools

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	npmConfig "github.com/qfdk/nginx-proxy-manager/app/config"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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
	var isRenew = false
	// 如果没有传入域名的话，默认是点击续签
	// 需要读取保存的domains 列表
	if len(domains) == 0 {
		data, _ := ioutil.ReadFile(path.Join(npmConfig.GetAppConfig().SSLPath, configName, "domains"))
		domains = strings.Split(string(data), ",")
		isRenew = true
	}

	myUser := MyUser{
		Email: npmConfig.GetAppConfig().Email,
		key:   privateKey,
	}

	config := lego.NewConfig(&myUser)

	// 不是生产环境用测试 URL
	if gin.Mode() != gin.ReleaseMode {
		config.CADirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	config.Certificate.KeyType = certcrypto.RSA2048

	// A client facilitates communication with the CA server.
	client, _ := lego.NewClient(config)

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

	// nginx 证书目录
	certificateSavedDir := filepath.Join(npmConfig.GetAppConfig().SSLPath, configName)
	if _, err := os.Stat(certificateSavedDir); os.IsNotExist(err) {
		os.MkdirAll(certificateSavedDir, 0755)
	}

	// Each certificate comes back with the cert bytes, the bytes of the client's
	ioutil.WriteFile(filepath.Join(certificateSavedDir, "fullchain.cer"),
		certificates.Certificate, 0644)
	// private key, and a certificate URL. SAVE THESE TO DISK.
	ioutil.WriteFile(filepath.Join(certificateSavedDir, "private.key"),
		certificates.PrivateKey, 0644)
	// 保存域名
	ioutil.WriteFile(filepath.Join(certificateSavedDir, "domains"),
		[]byte(strings.Join(domains, ",")), 0644)

	// 更新 redis 信息

	if isRenew {
		var output RedisData
		redisData := npmConfig.RedisGet(configName)
		json.Unmarshal([]byte(redisData), &output)
		if redisData != "" {
			dd, _ := time.ParseDuration("24h")
			output.Expired = time.Now().Add(dd * 80).Unix()
			res, _ := json.Marshal(output)
			npmConfig.RedisSet(configName, res)
		} else {
			fmt.Printf("没有找到 %v 的配置文件，需要保存新记录\n", configName)
			content, _ := ioutil.ReadFile(path.Join(npmConfig.GetAppConfig().VhostPath, configName+".conf"))
			npmConfig.SaveSiteDataInRedis(configName, domains, string(content), "")
		}
	}
	return nil
}
