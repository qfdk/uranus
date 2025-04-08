package models

import (
	"context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"log"
	"path"
	"sync"
	"time"
	"uranus/internal/config"
)

var (
	db   *gorm.DB
	once sync.Once
)

// InitWithContext initializes the database with context support for graceful shutdown
func InitWithContext(ctx context.Context) {
	once.Do(func() {
		log.Println("[+] Initializing SQLite ...")
		var err error
		dataDir := path.Join(config.GetAppConfig().InstallPath, "data.db")
		log.Println("SQLite location: " + dataDir)

		// Configure GORM with optimized settings
		db, err = gorm.Open(sqlite.Open(dataDir), &gorm.Config{
			PrepareStmt: true, // Cache prepared statements for better performance
			NowFunc: func() time.Time {
				return time.Now().UTC() // Use UTC for consistency
			},
		})
		if err != nil {
			log.Println("[-] Failed to initialize SQLite")
			panic(err)
		}

		// Get the underlying SQL DB to set connection pool settings
		sqlDB, err := db.DB()
		if err != nil {
			log.Println("[-] Failed to get DB connection")
			panic(err)
		}

		// Set connection pool settings for improved concurrent performance
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetConnMaxLifetime(time.Hour)

		// Auto migrate models
		AutoMigrate(&Cert{})

		log.Println("[+] SQLite initialization successful")

		// Listen for context cancellation to properly close DB
		go func() {
			<-ctx.Done()
			log.Println("[+] Closing SQLite connection...")
			sqlDB, _ := db.DB()
			if err := sqlDB.Close(); err != nil {
				log.Printf("[-] Error closing SQLite connection: %v", err)
			}
		}()
	})
}

// Init for backward compatibility
func Init() {
	// Create a background context that will never be canceled
	InitWithContext(context.Background())
}

func AutoMigrate(model interface{}) {
	err := db.AutoMigrate(model)
	if err != nil {
		log.Println(err)
	}
}

func GetDbClient() *gorm.DB {
	if db == nil {
		Init() // Initialize if not already done
	}
	return db
}
