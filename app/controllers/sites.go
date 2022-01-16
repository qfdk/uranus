package controllers

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetConfig  TODO /** 安全问题 ！！！跨目录
func GetConfig(ctx *gin.Context) {
	name := ctx.Query("name")
	path := filepath.Join(services.GetNginxConfPath(), name)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "siteEdit", gin.H{"configFileName": name, "content": string(content)})
}

func NewSite(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "siteEdit", gin.H{"content": ""})
}

func GetTemplate(ctx *gin.Context) {
	domain := ctx.Query("domain")
	enableSSL, _ := strconv.ParseBool(ctx.Query("ssl"))
	var templateConf = "http.conf"
	if enableSSL {
		templateConf = "https.conf"
	}
	content, err := ioutil.ReadFile(filepath.Join("template", templateConf))
	inputTemplate := string(content)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{domain}}", domain)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", config.GetAppConfig().SSLPath)
	if err != nil {
		fmt.Println(err)
	}
	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

func GetSites(ctx *gin.Context) {
	files, err := ioutil.ReadDir(filepath.Join(config.GetAppConfig().VhostPath))
	if err != nil {
		fmt.Println(err)
		ctx.HTML(http.StatusOK, "sites", gin.H{"files": []string{}})
		return
	}
	ctx.HTML(http.StatusOK, "sites", gin.H{"files": files, "humanizeBytes": humanize.Bytes})
}

func EditSiteConf(ctx *gin.Context) {
	name := ctx.Query("path")
	path := filepath.Join(config.GetAppConfig().VhostPath, name)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "edit", gin.H{"configFileName": name, "content": string(content)})
}

func DeleteSiteConf(ctx *gin.Context) {
	name := ctx.Query("path")
	path := filepath.Join(config.GetAppConfig().VhostPath, name)
	os.Remove(path)
	services.ReloadNginx()
	ctx.Redirect(http.StatusFound, "/sites")
}

func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("name")
	content := ctx.PostForm("content")
	if !strings.Contains(fileName, ".conf") {
		fileName = fileName + ".conf"
	}
	//  检测 文件夹是否存在不存在建立
	if _, err := os.Stat(config.GetAppConfig().VhostPath); os.IsNotExist(err) {
		os.MkdirAll(config.GetAppConfig().VhostPath, 0755)
	}
	path := filepath.Join(config.GetAppConfig().VhostPath, fileName)
	ioutil.WriteFile(path, []byte(content), 0644)
	response := services.ReloadNginx()
	ctx.JSON(http.StatusOK, gin.H{"message": response})
}
