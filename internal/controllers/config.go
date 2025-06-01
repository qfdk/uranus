package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"log"
	"net/http"
	"os"
	"path"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"
)

// GetConfigEditor renders the config editor page
func GetConfigEditor(ctx *gin.Context) {
	// Get config file path
	configPath := path.Join(tools.GetPWD(), "config.toml")

	// Read the content of the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading config file: %v", err)
		ctx.String(http.StatusInternalServerError, "Error reading configuration file")
		return
	}

	ctx.HTML(http.StatusOK, "configEdit.html", gin.H{
		"activePage": "app-config",
		"content":    string(content),
	})
}

// SaveConfig handles saving the edited configuration
func SaveConfig(ctx *gin.Context) {
	content, _ := ctx.GetPostForm("content")

	// Get config file path
	configPath := path.Join(tools.GetPWD(), "config.toml")

	// Create a timestamped backup file
	backupPath := configPath + ".backup." + time.Now().Format("20060102-150405")
	if err := copyFile(configPath, backupPath); err != nil {
		log.Printf("Error creating backup: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error creating backup: " + err.Error()})
		return
	}
	log.Printf("Configuration backed up to: %s", backupPath)

	// Validate the new content before saving
	tempConfigPath := configPath + ".temp"
	if err := os.WriteFile(tempConfigPath, []byte(content), 0644); err != nil {
		log.Printf("Error writing temporary config file: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error writing temporary configuration: " + err.Error()})
		return
	}

	// Test if the new configuration is valid
	tempViper := viper.New()
	tempViper.SetConfigFile(tempConfigPath)
	tempViper.SetConfigType("toml")
	
	if err := tempViper.ReadInConfig(); err != nil {
		// Clean up temp file and restore from backup if needed
		os.Remove(tempConfigPath)
		log.Printf("Invalid configuration format: %v", err)
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "Invalid configuration format: " + err.Error()})
		return
	}

	// Configuration is valid, atomically replace the original file
	if err := os.Rename(tempConfigPath, configPath); err != nil {
		// If rename fails, try copy and remove
		if copyErr := copyFile(tempConfigPath, configPath); copyErr != nil {
			log.Printf("Error saving config file: %v", copyErr)
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error saving configuration: " + copyErr.Error()})
			return
		}
		os.Remove(tempConfigPath)
	}

	// 不需要重新设置viper配置，这会干扰全局状态
	// 直接强制重新加载应用配置缓存
	log.Printf("Configuration file updated successfully, reloading app config cache")
	config.ReloadConfig()

	log.Printf("Configuration successfully updated and reloaded")
	ctx.JSON(http.StatusOK, gin.H{"message": "Configuration updated successfully"})
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, input, 0644)
}
