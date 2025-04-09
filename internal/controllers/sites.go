package controllers

import (
	_ "embed"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

// 模板缓存，用于防止重复的字符串操作
var (
	templateCache     = make(map[string]string)
	templateCacheLock sync.RWMutex
)

// NewSite 创建新站点的页面处理
func NewSite(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
		"configFileName": "",
		"content":        "",
		"isNewSite":      true,
		"infoPlus":       true,
		"isDefaultConf":  false,
	})
}

// GetTemplate 获取配置模板
func GetTemplate(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	proxy := ctx.Query("proxy")
	enableSSL, _ := strconv.ParseBool(ctx.Query("ssl"))

	// 根据参数创建缓存键
	cacheKey := strings.Join(domains, ",") + "|" + configName + "|" + proxy + "|" + strconv.FormatBool(enableSSL)

	// 首先检查模板缓存
	templateCacheLock.RLock()
	if cachedTemplate, ok := templateCache[cacheKey]; ok {
		templateCacheLock.RUnlock()
		ctx.JSON(http.StatusOK, gin.H{"content": cachedTemplate})
		return
	}
	templateCacheLock.RUnlock()

	// 缓存未命中，生成模板
	var templateConf string
	if enableSSL {
		templateConf = httpsConf
	} else {
		templateConf = httpConf
	}

	// 应用模板替换
	inputTemplate := templateConf
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{domain}}", strings.Join(domains, " "))
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{configName}}", configName)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", GetAppConfig().SSLPath)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{proxy}}", proxy)

	// 更新缓存
	templateCacheLock.Lock()
	templateCache[cacheKey] = inputTemplate
	templateCacheLock.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

// GetSites 获取所有站点配置
func GetSites(ctx *gin.Context) {
	vhostPath := GetAppConfig().VhostPath
	allFiles, err := ioutil.ReadDir(vhostPath)
	if err != nil {
		log.Println(err)
		ctx.HTML(http.StatusOK, "sites.html", gin.H{
			"activePage": "sites",
			"files":      []string{}})
		return
	}

	// 只过滤显示.conf文件
	var confFiles []os.FileInfo
	for _, file := range allFiles {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".conf") {
			confFiles = append(confFiles, file)
		}
	}

	ctx.HTML(http.StatusOK, "sites.html", gin.H{
		"files":         confFiles,
		"humanizeBytes": humanize.Bytes,
		"activePage":    "sites",
	})
}

// EditSiteConf 编辑站点配置
func EditSiteConf(ctx *gin.Context) {
	// 从URL参数获取文件名
	filename := ctx.Param("filename")

	// 确保文件名具有.conf扩展名用于文件读取
	fileToRead := filename
	if !strings.HasSuffix(fileToRead, ".conf") && filename != "default" {
		fileToRead = filename + ".conf"
	}

	// 提取不带扩展名的配置名称
	configName := filename
	if strings.HasSuffix(configName, ".conf") {
		configName = strings.TrimSuffix(configName, ".conf")
	}

	vhostPath := GetAppConfig().VhostPath
	filePath := filepath.Join(vhostPath, fileToRead)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("未找到配置文件: %v", filePath)
		ctx.String(http.StatusNotFound, "未找到配置文件")
		return
	}

	// 读取默认配置
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("读取默认配置出错: %v", err)
		ctx.String(http.StatusInternalServerError, "读取配置出错")
		return
	}

	if filename != "default" {
		// 从数据库获取证书信息
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
		ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
			"configFileName": configName,
			"content":        string(content),
			"infoPlus":       false,
			"isDefaultConf":  true,
		})
	}
}

// DeleteSiteConf 删除站点配置
func DeleteSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")

	// 确保文件名具有.conf扩展名用于文件删除
	fileToDelete := filename
	if !strings.HasSuffix(fileToDelete, ".conf") && filename != "default" {
		fileToDelete = filename + ".conf"
	}

	// 提取不带扩展名的配置名称
	configName := filename
	if strings.HasSuffix(configName, ".conf") {
		configName = strings.TrimSuffix(configName, ".conf")
	}

	// 删除配置文件
	vhostPath := GetAppConfig().VhostPath
	err := os.Remove(filepath.Join(vhostPath, fileToDelete))
	if err != nil {
		log.Printf("删除配置文件出错: %v", err)
	}

	// 如果存在，删除SSL目录
	sslPath := GetAppConfig().SSLPath
	err = os.RemoveAll(filepath.Join(sslPath, configName))
	if err != nil {
		log.Printf("删除SSL目录出错: %v", err)
	}

	// 从数据库中删除
	cert := models.GetCertByFilename(configName)
	err = cert.Remove()
	if err != nil {
		log.Println(err)
	}

	// 清除所有缓存以确保数据刷新
	templateCacheLock.Lock()
	templateCache = make(map[string]string)
	templateCacheLock.Unlock()

	// 重新加载nginx
	services.ReloadNginx()

	ctx.Redirect(http.StatusFound, "/admin/sites")
}

// SaveSiteConf 保存站点配置
func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("filename")
	domains := ctx.PostFormArray("domains[]")
	content := ctx.PostForm("content")
	proxy := ctx.PostForm("proxy")

	// 如果不是默认配置，则保存到数据库
	if fileName != "default" {
		cert := models.GetCertByFilename(fileName)
		cert.Content = content
		cert.Domains = strings.Join(domains, ",")
		cert.FileName = fileName
		cert.Proxy = proxy
		models.GetDbClient().Save(&cert)
	}

	// 准备用于文件路径的文件名，确保具有.conf扩展名
	fullFileName := fileName
	if !strings.HasSuffix(fileName, ".conf") {
		fullFileName = fileName + ".conf"
	}

	// 确保vhost目录存在
	vhostPath := GetAppConfig().VhostPath
	if _, err := os.Stat(vhostPath); os.IsNotExist(err) {
		err = os.MkdirAll(vhostPath, 0755)
		if err != nil {
			log.Printf("创建vhost目录出错: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "创建目录出错"})
			return
		}
	}

	// 写入配置文件
	filePath := filepath.Join(vhostPath, fullFileName)
	err := ioutil.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		log.Printf("写入配置文件出错: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "写入文件出错"})
		return
	}

	// 清除所有缓存以确保数据刷新
	templateCacheLock.Lock()
	templateCache = make(map[string]string)
	templateCacheLock.Unlock()

	// 重新加载nginx
	response := services.ReloadNginx()
	ctx.JSON(http.StatusOK, gin.H{"message": response})
}
