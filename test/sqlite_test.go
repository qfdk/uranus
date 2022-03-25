package test

import (
	"gorm.io/driver/sqlite" // Sqlite driver based on GGO
	// "github.com/glebarez/sqlite" // Pure go SQLite driver, checkout https://github.com/glebarez/sqlite for details
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"testing"
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

func TestSQLite(t *testing.T) {
	// github.com/mattn/go-sqlite3
	db, err := gorm.Open(sqlite.Open("gorm.db"), &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Info),
		PrepareStmt: true,
	})
	if err != nil {
		log.Println(err)
	}
	//AutoMigrate(&Model{})
	AutoMigrate(db, &Cert{})

	//db.Model(&Cert{}).Where("id = ?", 1).Update("cert", "yooo")
}

func AutoMigrate(db *gorm.DB, model interface{}) {
	err := db.AutoMigrate(model)
	if err != nil {
		log.Fatal(err)
	}
}
