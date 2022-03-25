package config

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"nginx-proxy-manager/app/models"
)

var db *gorm.DB

func Init() {
	log.Println("[+] 初始化 SQLite ...")
	var err error
	db, err = gorm.Open(sqlite.Open("data.db"), &gorm.Config{
		//Logger:      logger.Default.LogMode(logger.Info),
		PrepareStmt: true,
	})
	if err != nil {
		log.Println("[-] 初始化 SQLite 失败")
	}
	// Migrate the schema
	AutoMigrate(&models.Cert{})
	log.Println("[+] 初始化 SQLite 成功")
}

func AutoMigrate(model interface{}) {
	err := db.AutoMigrate(model)
	if err != nil {
		log.Println(err)
	}
}
func GetDbClient() *gorm.DB {
	return db
}
