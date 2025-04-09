package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"io/ioutil"
	"log"
	"net/http"
	"path"
	"uranus/internal/config"
	"uranus/internal/tools"
)

// GetConfigEditor renders the config editor page
func GetConfigEditor(ctx *gin.Context) {
	// Get config file path
	configPath := path.Join(tools.GetPWD(), "config.toml")

	// Read the content of the config file
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Printf("Error reading config file: %v", err)
		ctx.String(http.StatusInternalServerError, "Error reading configuration file")
		return
	}

	ctx.HTML(http.StatusOK, "configEdit.html", gin.H{
		"activePage": "config",
		"content":    string(content),
	})
}

// SaveConfig handles saving the edited configuration
func SaveConfig(ctx *gin.Context) {
	content, _ := ctx.GetPostForm("content")

	// Get config file path
	configPath := path.Join(tools.GetPWD(), "config.toml")

	// Backup the existing config file
	backupPath := configPath + ".bak"
	if err := copyFile(configPath, backupPath); err != nil {
		log.Printf("Error creating backup: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error creating backup: " + err.Error()})
		return
	}

	// Write the new content to the config file
	err := ioutil.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		log.Printf("Error saving config file: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error saving configuration: " + err.Error()})
		return
	}

	// Reload the configuration
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Error reloading configuration: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error reloading configuration: " + err.Error()})
		return
	}

	// Update the app config
	if err := viper.Unmarshal(config.GetAppConfig()); err != nil {
		log.Printf("Error unmarshalling config: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error applying configuration: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "OK"})
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	input, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dst, input, 0644)
}
