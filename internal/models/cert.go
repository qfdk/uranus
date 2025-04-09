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

// GetCertByFilename 根据文件名获取证书
func GetCertByFilename(filename string) (cert Cert) {
	// 确保删除可能存在的.conf后缀
	cleanFilename := filename
	if strings.HasSuffix(cleanFilename, ".conf") {
		cleanFilename = strings.TrimSuffix(cleanFilename, ".conf")
	}
	GetDbClient().Find(&cert, "file_name = ?", cleanFilename)
	return
}

// Remove 从数据库中删除证书
func (c *Cert) Remove() error {
	return GetDbClient().Where("file_name", c.FileName).Unscoped().Delete(c).Error
}
