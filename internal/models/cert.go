package models

import (
	"gorm.io/gorm"
	"strings"
	"time"
)

// Cert 证书模型结构体
type Cert struct {
	gorm.Model
	Content  string    `json:"content"`
	NotAfter time.Time `json:"notAfter"`
	Domains  string    `json:"domains"`
	FileName string    `json:"fileName"`
	Proxy    string    `json:"proxy"`
}

// GetCertificates 获取所有证书
func GetCertificates() (certs []Cert) {
	GetDbClient().Find(&certs)
	return
}

// IssuedCertificates 返回已经签发了证书的网站。
// 每个网站保存时都会有一条记录，签发证书之后 NotAfter 才有值
func IssuedCertificates() (certs []Cert) {
	for _, cert := range GetCertificates() {
		if !cert.NotAfter.IsZero() {
			certs = append(certs, cert)
		}
	}
	return
}

// GetCertByFilename 根据文件名获取证书
func GetCertByFilename(filename string) (cert Cert) {
	// 确保删除可能存在的.conf后缀
	if strings.HasSuffix(filename, ".conf") {
		filename = strings.TrimSuffix(filename, ".conf")
	}
	GetDbClient().Find(&cert, "file_name = ?", filename)
	return
}

// Remove 从数据库中删除证书
func (c *Cert) Remove() error {
	return GetDbClient().Where("file_name", c.FileName).Unscoped().Delete(c).Error
}
