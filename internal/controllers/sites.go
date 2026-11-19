package controllers

import (
	_ "embed"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
		"activePage":     "sites",
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
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", SSLDir())
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{proxy}}", proxy)

	// 更新缓存
	templateCacheLock.Lock()
	templateCache[cacheKey] = inputTemplate
	templateCacheLock.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

// siteFile 是网站列表中的一个配置文件
type siteFile struct {
	Name    string
	Size    string
	ModTime time.Time
}

// GetSites 获取所有站点配置
func GetSites(ctx *gin.Context) {
	sites := Sites()
	entries, err := os.ReadDir(sites.Dir)
	if err != nil {
		log.Println(err)
	}

	// 只显示 .conf 文件。模板需要大小和修改时间，os.DirEntry 本身没有这些字段
	files := []siteFile{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, siteFile{
			Name:    entry.Name(),
			Size:    humanize.Bytes(uint64(info.Size())),
			ModTime: info.ModTime(),
		})
	}

	ctx.HTML(http.StatusOK, "sites.html", gin.H{
		"activePage": "sites",
		"files":      files,
		"sitesDir":   sites.Dir,
		// nginx 主配置没有引用网站目录时，这里的网站不会生效
		"notIncluded": sites.Checked && !sites.Included,
		"nginxConf":   ReadNginxCompileInfo().NginxConfPath,
		// 删除网站、添加 include 之后带回来的结果
		"deleted":  ctx.Query("deleted"),
		"message":  ctx.Query("message"),
		"included": ctx.Query("included"),
	})
}

// IncludeSitesDir adds the sites directory to the http block of nginx.conf.
func IncludeSitesDir(ctx *gin.Context) {
	query := url.Values{"included": {services.IncludeSitesDir()}}
	ctx.Redirect(http.StatusFound, "/admin/sites?"+query.Encode())
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

	vhostPath := SitesDir()
	filePath := filepath.Join(vhostPath, fileToRead)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("未找到配置文件: %v", filePath)
		ctx.String(http.StatusNotFound, "未找到配置文件")
		return
	}

	// 读取默认配置
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("读取默认配置出错: %v", err)
		ctx.String(http.StatusInternalServerError, "读取配置出错")
		return
	}

	if filename != "default" {
		// 从数据库获取证书信息
		cert := models.GetCertByFilename(configName)
		ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
			"activePage":     "sites",
			"configFileName": configName,
			"domains":        cert.Domains,
			"content":        string(content),
			"proxy":          cert.Proxy,
			"infoPlus":       true,
			"isDefaultConf":  false,
		})
	} else {
		ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{
			"activePage":     "sites",
			"configFileName": configName,
			"content":        string(content),
			"infoPlus":       false,
			"isDefaultConf":  true,
		})
	}
}

// validSiteName 检查网站配置名称：它会成为网站目录里的文件名和证书目录里的目录名，不能带路径
func validSiteName(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.HasPrefix(name, ".")
}

// DeleteSiteConf 删除网站配置和它的证书，然后重新加载 Nginx
func DeleteSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.TrimSuffix(filename, ".conf")
	if !validSiteName(configName) {
		ctx.String(http.StatusBadRequest, "配置名称不正确")
		return
	}

	// 确保文件名具有.conf扩展名用于文件删除
	fileToDelete := configName + ".conf"
	if filename == "default" {
		fileToDelete = filename
	}

	// 删除配置文件
	vhostPath := SitesDir()
	err := os.Remove(filepath.Join(vhostPath, fileToDelete))
	if err != nil {
		log.Printf("删除配置文件出错: %v", err)
	}

	// 如果存在，删除SSL目录
	sslPath := SSLDir()
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

	// 重新加载失败时 Nginx 还在用旧配置提供这个网站，要告诉用户
	query := url.Values{"deleted": {fileToDelete}, "message": {services.ReloadNginx()}}
	ctx.Redirect(http.StatusFound, "/admin/sites?"+query.Encode())
}

// SaveSiteConf 保存站点配置：写入后用 nginx -t 检查，不通过就撤销这次修改
func SaveSiteConf(ctx *gin.Context) {
	// 配置名称就是文件名（不含 .conf），不能带路径
	fileName := strings.TrimSuffix(strings.TrimSpace(ctx.PostForm("filename")), ".conf")
	if !validSiteName(fileName) {
		ctx.JSON(http.StatusOK, gin.H{"message": "配置名称不能为空，也不能包含路径，例如 example.com"})
		return
	}
	domains := ctx.PostFormArray("domains[]")
	content := ctx.PostForm("content")
	proxy := ctx.PostForm("proxy")

	// 确保vhost目录存在
	vhostPath := SitesDir()
	if err := os.MkdirAll(vhostPath, 0755); err != nil {
		log.Printf("创建vhost目录出错: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "创建目录出错"})
		return
	}

	result := services.WriteConfigChecked(filepath.Join(vhostPath, fileName+".conf"), []byte(content))

	// 配置确实保存下来之后才更新数据库里的网站信息
	if result == "OK" && fileName != "default" {
		cert := models.GetCertByFilename(fileName)
		cert.Content = content
		cert.Domains = strings.Join(domains, ",")
		cert.FileName = fileName
		cert.Proxy = proxy
		models.GetDbClient().Save(&cert)
	}

	// 清除所有缓存以确保数据刷新
	templateCacheLock.Lock()
	templateCache = make(map[string]string)
	templateCacheLock.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"message": result})
}
