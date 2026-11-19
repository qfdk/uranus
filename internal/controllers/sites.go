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

// siteRow 是网站列表中的一行
type siteRow struct {
	Name    string
	Path    string
	Dir     string
	Link    string
	Size    string
	ModTime time.Time
	// DisabledID 是停用网站的记录编号，启用中的网站为 0
	DisabledID uint
}

// GetSites 列出 nginx 加载的所有网站：Debian、Ubuntu 同时加载 conf.d/*.conf 和 sites-enabled/*
func GetSites(ctx *gin.Context) {
	sites := Sites()
	rows := []siteRow{}
	for _, site := range SiteFiles() {
		rows = append(rows, siteRow{
			Name:    site.Name,
			Path:    site.Path,
			Dir:     filepath.Dir(site.Path),
			Link:    site.Link,
			Size:    humanize.Bytes(uint64(site.Size)),
			ModTime: site.ModTime,
		})
	}

	disabled := []siteRow{}
	for _, record := range models.DisabledSites() {
		disabled = append(disabled, siteRow{
			Name:       filepath.Base(record.Path),
			Path:       record.Path,
			Dir:        filepath.Dir(record.Path),
			Link:       record.Link,
			ModTime:    record.CreatedAt.Local(),
			DisabledID: record.ID,
		})
	}

	ctx.HTML(http.StatusOK, "sites.html", gin.H{
		"activePage": "sites",
		"files":      rows,
		"disabled":   disabled,
		"sitesDir":   sites.Dir,
		// nginx 主配置没有引用网站目录时，这里的网站不会生效
		"notIncluded": sites.Checked && !sites.Included,
		"nginxConf":   ReadNginxCompileInfo().NginxConfPath,
		// 删除网站、添加 include 之后带回来的结果
		"deleted":      ctx.Query("deleted"),
		"message":      ctx.Query("message"),
		"certKept":     ctx.Query("certKept"),
		"linkKept":     ctx.Query("linkKept"),
		"included":     ctx.Query("included"),
		"enabled":      ctx.Query("enabled"),
		"disabledName": ctx.Query("disabled"),
	})
}

// IncludeSitesDir adds the sites directory to the http block of nginx.conf.
func IncludeSitesDir(ctx *gin.Context) {
	query := url.Values{"included": {services.IncludeSitesDir()}}
	ctx.Redirect(http.StatusFound, "/admin/sites?"+query.Encode())
}

// managedSite reports whether Uranus created the site: name.conf, with its
// domains and proxy in the database. Files like Debian's sites-enabled/default
// are only edited as text.
func managedSite(site SiteFile) bool {
	return strings.HasSuffix(site.Name, ".conf") && site.ConfigName() != "default"
}

// EditSiteConf 编辑站点配置。路径必须是网站列表里的文件
func EditSiteConf(ctx *gin.Context) {
	site, ok := FindSiteFile(ctx.Query("path"))
	if !ok {
		ctx.String(http.StatusNotFound, "没有这个网站")
		return
	}
	content, err := os.ReadFile(site.Path)
	if err != nil {
		log.Printf("读取网站配置出错: %v", err)
		ctx.String(http.StatusInternalServerError, "读取配置出错")
		return
	}

	data := gin.H{
		"activePage":     "sites",
		"configFileName": site.ConfigName(),
		"sitePath":       site.Path,
		"siteLink":       site.Link,
		"content":        string(content),
		"infoPlus":       false,
		"isDefaultConf":  true,
	}
	if managedSite(site) {
		cert := models.GetCertByFilename(site.ConfigName())
		data["domains"], data["proxy"] = cert.Domains, cert.Proxy
		data["infoPlus"], data["isDefaultConf"] = true, false
	}
	ctx.HTML(http.StatusOK, "siteConfEdit.html", data)
}

// validSiteName 检查网站配置名称：它会成为网站目录里的文件名和证书目录里的目录名，不能带路径
func validSiteName(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.HasPrefix(name, ".")
}

// removeUnusedCertificate deletes the certificate of a deleted site, unless
// another configuration (enabled or disabled) still uses it: without its files
// the next reload would fail, without its record it would not be renewed.
// Returns the configurations that keep it.
func removeUnusedCertificate(configName string) []string {
	if !validSiteName(configName) {
		return nil
	}
	if kept := services.CertificateUsers(configName); len(kept) > 0 {
		log.Printf("证书 %s 还在被 %s 使用，已保留", configName, strings.Join(kept, ", "))
		return kept
	}
	if err := os.RemoveAll(filepath.Join(SSLDir(), configName)); err != nil {
		log.Printf("删除SSL目录出错: %v", err)
	}
	cert := models.GetCertByFilename(configName)
	if err := cert.Remove(); err != nil {
		log.Println(err)
	}
	return nil
}

// sitesResult goes back to the site list with the result of an action.
func sitesResult(ctx *gin.Context, query url.Values) {
	ctx.Redirect(http.StatusFound, "/admin/sites?"+query.Encode())
}

// DisableSiteConf 停用网站：Nginx 不再加载它，配置保留，可以重新启用
func DisableSiteConf(ctx *gin.Context) {
	site, ok := FindSiteFile(ctx.PostForm("path"))
	if !ok {
		ctx.String(http.StatusBadRequest, "没有这个网站")
		return
	}
	sitesResult(ctx, url.Values{"disabled": {site.Name}, "message": {services.DisableSite(site)}})
}

// disabledSite returns the disabled site named by the id field of the form.
func disabledSite(ctx *gin.Context) (models.DisabledSite, bool) {
	id, err := strconv.ParseUint(ctx.PostForm("id"), 10, 0)
	if err != nil {
		return models.DisabledSite{}, false
	}
	return models.FindDisabledSite(uint(id))
}

// EnableSiteConf 重新启用停用的网站
func EnableSiteConf(ctx *gin.Context) {
	record, ok := disabledSite(ctx)
	if !ok {
		ctx.String(http.StatusBadRequest, "没有这个停用的网站")
		return
	}
	sitesResult(ctx, url.Values{"enabled": {filepath.Base(record.Path)}, "message": {services.EnableSite(record.ID)}})
}

// DeleteDisabledSiteConf 删除停用的网站和它不再被使用的证书。Nginx 没有加载它，不用重新加载
func DeleteDisabledSiteConf(ctx *gin.Context) {
	record, ok := disabledSite(ctx)
	if !ok {
		ctx.String(http.StatusBadRequest, "没有这个停用的网站")
		return
	}
	name := filepath.Base(record.Path)
	if err := services.DeleteDisabledSite(record); err != nil {
		log.Printf("删除停用的网站出错: %v", err)
		sitesResult(ctx, url.Values{"deleted": {name}, "message": {"删除配置文件失败：" + err.Error()}})
		return
	}
	query := url.Values{"deleted": {name}, "message": {"OK"}}
	if kept := removeUnusedCertificate(strings.TrimSuffix(name, ".conf")); len(kept) > 0 {
		query.Set("certKept", strings.Join(kept, "\n"))
	}
	if record.Link != "" {
		query.Set("linkKept", record.Link)
	}
	sitesResult(ctx, query)
}

// DeleteSiteConf 删除网站配置和它的证书，然后重新加载 Nginx。路径必须是网站列表里的文件
func DeleteSiteConf(ctx *gin.Context) {
	site, ok := FindSiteFile(ctx.PostForm("path"))
	if !ok {
		ctx.String(http.StatusBadRequest, "没有这个网站")
		return
	}

	// sites-enabled 里多是指向 sites-available 的链接：只删链接，相当于停用网站，原文件保留
	if err := os.Remove(site.Path); err != nil {
		log.Printf("删除配置文件出错: %v", err)
		query := url.Values{"deleted": {site.Name}, "message": {"删除配置文件失败：" + err.Error()}}
		ctx.Redirect(http.StatusFound, "/admin/sites?"+query.Encode())
		return
	}

	certKept := removeUnusedCertificate(site.ConfigName())

	// 清除所有缓存以确保数据刷新
	templateCacheLock.Lock()
	templateCache = make(map[string]string)
	templateCacheLock.Unlock()

	// 重新加载失败时 Nginx 还在用旧配置提供这个网站，要告诉用户
	query := url.Values{"deleted": {site.Name}, "message": {services.ReloadNginx()}}
	if len(certKept) > 0 {
		query.Set("certKept", strings.Join(certKept, "\n"))
	}
	if site.Link != "" {
		query.Set("linkKept", site.Link)
	}
	sitesResult(ctx, query)
}

// SaveSiteConf 保存站点配置：写入后用 nginx -t 检查，不通过就撤销这次修改。
// 已有的网站写回它自己的文件（可能在 sites-enabled），新网站保存到网站目录
func SaveSiteConf(ctx *gin.Context) {
	content := ctx.PostForm("content")
	var target, configName string
	managed := true
	if path := ctx.PostForm("path"); path != "" {
		site, ok := FindSiteFile(path)
		if !ok {
			ctx.JSON(http.StatusOK, gin.H{"message": "没有这个网站，可能已经被删除"})
			return
		}
		target, configName, managed = site.Path, site.ConfigName(), managedSite(site)
	} else {
		// 配置名称就是文件名（不含 .conf），不能带路径
		configName = strings.TrimSuffix(strings.TrimSpace(ctx.PostForm("filename")), ".conf")
		if !validSiteName(configName) {
			ctx.JSON(http.StatusOK, gin.H{"message": "配置名称不能为空，也不能包含路径，例如 example.com"})
			return
		}
		// 同名网站已经存在时不覆盖：可能在另一个目录，两份 server 会冲突
		for _, site := range SiteFiles() {
			if site.ConfigName() == configName {
				ctx.JSON(http.StatusOK, gin.H{"message": "已经有同名网站 " + site.Path + "，请在网站列表里编辑它"})
				return
			}
		}
		if err := os.MkdirAll(SitesDir(), 0755); err != nil {
			log.Printf("创建网站目录出错: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "创建目录出错"})
			return
		}
		target = filepath.Join(SitesDir(), configName+".conf")
		managed = configName != "default"
	}

	result := services.WriteConfigChecked(target, []byte(content))

	// 配置确实保存下来之后才更新数据库里的网站信息
	if result == "OK" && managed {
		cert := models.GetCertByFilename(configName)
		cert.Content = content
		cert.Domains = strings.Join(ctx.PostFormArray("domains[]"), ",")
		cert.FileName = configName
		cert.Proxy = ctx.PostForm("proxy")
		models.GetDbClient().Save(&cert)
	}

	// 清除所有缓存以确保数据刷新
	templateCacheLock.Lock()
	templateCache = make(map[string]string)
	templateCacheLock.Unlock()

	ctx.JSON(http.StatusOK, gin.H{"message": result})
}
