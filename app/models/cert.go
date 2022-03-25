package models

import (
	"gorm.io/gorm"
	"nginx-proxy-manager/app/config"
	"time"
)

type Cert struct {
	gorm.Model
	Content  string    `json:"content"`
	NotAfter time.Time `json:"notAfter"`
	Domains  string    `json:"domains"`
	FileName string    `json:"fileName"`
	Proxy    string    `json:"proxy"`
}

func GetCertificates() (certs []Cert) {
	config.GetDbClient().Find(&certs)
	return
}

func GetCertByFilename(filename string) (cert Cert) {
	config.GetDbClient().Find(&cert, "file_name = ?", filename)
	return
}

func (c *Cert) Remove() error {
	return config.GetDbClient().Where("file_name", c.FileName).Unscoped().Delete(c).Error
}
