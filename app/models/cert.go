package models

import (
	"gorm.io/gorm"
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
	GetDbClient().Find(&certs)
	return
}

func GetCertByFilename(filename string) (cert Cert) {
	GetDbClient().Find(&cert, "file_name = ?", filename)
	return
}

func (c *Cert) Remove() error {
	return GetDbClient().Where("file_name", c.FileName).Unscoped().Delete(c).Error
}
