package models

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"path"
	"uranus/internal/config"
)

var db *gorm.DB

func Init() {
	log.Println("[+] 初始化 SQLite ...")
	var err error
	dataDir := path.Join(config.GetAppConfig().InstallPath, "data.db")
	log.Println("SQLite 位置 : " + dataDir)
	db, err = gorm.Open(sqlite.Open(dataDir), &gorm.Config{
		//Logger:      logger.Default.LogMode(logger.Info),
		PrepareStmt: true,
	})
	if err != nil {
		log.Println("[-] 初始化 SQLite 失败")
		panic(err)
	}
	// Migrate the schema
	AutoMigrate(&Cert{})
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
