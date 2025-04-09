package controllers

import (
	_ "embed"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	. "uranus/internal/config"
	"uranus/internal/models"
	"uranus/internal/services"
)

//go:embed template/http.conf
var httpConf string

//go:embed template/https.conf
var httpsConf string

// Template cache to prevent repeated string operations
var (
	templateCache     = make(map[string]string)
	templateCacheLock sync.RWMutex
)

func NewSite(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
		"configFileName": "",
		"content":        "",
		"isNewSite":      true,
		"infoPlus":       true,
		"isDefaultConf":  false,
	})
}

func GetTemplate(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	proxy := ctx.Query("proxy")
	enableSSL, _ := strconv.ParseBool(ctx.Query("ssl"))

	// Create cache key from parameters
	cacheKey := strings.Join(domains, ",") + "|" + configName + "|" + proxy + "|" + strconv.FormatBool(enableSSL)

	// Check template cache first
	templateCacheLock.RLock()
	if cachedTemplate, ok := templateCache[cacheKey]; ok {
		templateCacheLock.RUnlock()
		ctx.JSON(http.StatusOK, gin.H{"content": cachedTemplate})
		return
	}
	templateCacheLock.RUnlock()

	// Cache miss, generate template
	var templateConf string
	if enableSSL {
		templateConf = httpsConf
	} else {
		templateConf = httpConf
	}

	// Apply template replacements
	inputTemplate := templateConf
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{domain}}", strings.Join(domains, " "))
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{configName}}", configName)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", GetAppConfig().SSLPath)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{proxy}}", proxy)

	// Update cache
	templateCacheLock.Lock()
	templateCache[cacheKey] = inputTemplate
	templateCacheLock.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

func GetSites(ctx *gin.Context) {
	vhostPath := GetAppConfig().VhostPath
	files, err := ioutil.ReadDir(filepath.Join(vhostPath))
	if err != nil {
		log.Println(err)
		ctx.HTML(http.StatusOK, "sites.html", gin.H{
			"activePage": "sites",
			"files":      []string{}})
		return
	}

	// Pre-format file sizes for performance
	ctx.HTML(http.StatusOK, "sites.html", gin.H{
		"files":         files,
		"humanizeBytes": humanize.Bytes,
	})
}

// TODO: fixme 这里要修改前端还有后端 保存的时候有问题
func EditSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]
	if filename != "default" {
		// Read default configuration from file
		content, err := ioutil.ReadFile(path.Join(GetAppConfig().VhostPath, filename))
		if err != nil {
			log.Printf("Error reading default configuration: %v", err)
			ctx.String(http.StatusInternalServerError, "Error reading configuration")
			return
		}

		// Get certificate info from database
		cert := models.GetCertByFilename(configName)
		ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
			"configFileName": configName,
			"domains":        cert.Domains,
			"content":        string(content),
			"proxy":          cert.Proxy,
			"infoPlus":       true,
			"isDefaultConf":  false,
		})
	} else {
		// Read default configuration from file
		content, err := ioutil.ReadFile(path.Join(GetAppConfig().VhostPath, filename))
		if err != nil {
			log.Printf("Error reading default configuration: %v", err)
			ctx.String(http.StatusInternalServerError, "Error reading configuration")
			return
		}

		ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
			"configFileName": configName,
			"content":        string(content),
			"infoPlus":       false,
			"isDefaultConf":  true,
		})
	}
}

func DeleteSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]

	// Remove configuration file
	vhostPath := GetAppConfig().VhostPath
	err := os.Remove(filepath.Join(vhostPath, filename))
	if err != nil {
		log.Printf("Error deleting configuration file: %v", err)
	}

	// Remove SSL directory if exists
	sslPath := GetAppConfig().SSLPath
	err = os.RemoveAll(filepath.Join(sslPath, configName))
	if err != nil {
		log.Printf("Error deleting SSL directory: %v", err)
	}

	// Delete from database
	cert := models.GetCertByFilename(configName)
	err = cert.Remove()
	if err != nil {
		log.Println(err)
	}

	// Invalidate template cache
	templateCacheLock.Lock()
	for key := range templateCache {
		if strings.Contains(key, configName) {
			delete(templateCache, key)
		}
	}
	templateCacheLock.Unlock()

	// Reload nginx
	services.ReloadNginx()

	ctx.Redirect(http.StatusFound, "/admin/sites")
}

func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("filename")
	domains := ctx.PostFormArray("domains[]")
	content := ctx.PostForm("content")
	proxy := ctx.PostForm("proxy")

	// Save to database if not default configuration
	if fileName != "default" {
		cert := models.GetCertByFilename(fileName)
		cert.Content = content
		cert.Domains = strings.Join(domains, ",")
		cert.FileName = fileName
		cert.Proxy = proxy
		models.GetDbClient().Save(&cert)
	}

	// Prepare filename for file path
	fullFileName := fileName
	if fileName != "default" {
		fullFileName = fileName + ".conf"
	}

	// Ensure vhost directory exists
	vhostPath := GetAppConfig().VhostPath
	if _, err := os.Stat(vhostPath); os.IsNotExist(err) {
		err = os.MkdirAll(vhostPath, 0755)
		if err != nil {
			log.Printf("Error creating vhost directory: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error creating directory"})
			return
		}
	}

	// Write configuration file
	filePath := filepath.Join(vhostPath, fullFileName)
	err := ioutil.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		log.Printf("Error writing configuration file: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "Error writing file"})
		return
	}

	// Invalidate template cache
	templateCacheLock.Lock()
	for key := range templateCache {
		if strings.Contains(key, fileName) {
			delete(templateCache, key)
		}
	}
	templateCacheLock.Unlock()

	// Reload nginx
	response := services.ReloadNginx()
	ctx.JSON(http.StatusOK, gin.H{"message": response})
}
