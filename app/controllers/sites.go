package controllers

import (
	_ "embed"
	"encoding/json"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	. "github.com/qfdk/nginx-proxy-manager/app/config"
	"github.com/qfdk/nginx-proxy-manager/app/services"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed template/http.conf
var httpConf string

//go:embed template/https.conf
var httpsConf string

func NewSite(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "siteConfEdit.html", gin.H{"configFileName": "", "content": "", "isNewSite": true, "infoPlus": GetAppConfig().Redis})
}

func GetTemplate(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	proxy := ctx.Query("proxy")
	enableSSL, _ := strconv.ParseBool(ctx.Query("ssl"))
	var templateConf = httpConf
	if enableSSL {
		templateConf = httpsConf
	}
	inputTemplate := templateConf
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{domain}}", strings.Join(domains[:], " "))
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{configName}}", configName)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", GetAppConfig().SSLPath)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{proxy}}", proxy)
	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

func GetSites(ctx *gin.Context) {
	files, err := ioutil.ReadDir(filepath.Join(GetAppConfig().VhostPath))
	if err != nil {
		log.Println(err)
		ctx.HTML(http.StatusOK, "sites.html", gin.H{"files": []string{}})
		return
	}
	ctx.HTML(http.StatusOK, "sites.html", gin.H{"files": files, "humanizeBytes": humanize.Bytes})
}

func EditSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]
	if filename != "default" && GetAppConfig().Redis {
		redisData, _ := RedisClient.Get(RedisPrefix + configName).Result()
		var output gin.H
		json.Unmarshal([]byte(redisData), &output)
		ctx.HTML(http.StatusOK, "siteConfEdit.html",
			gin.H{
				"configFileName": output["fileName"],
				"domains":        output["domains"],
				"content":        output["content"],
				"proxy":          output["proxy"],
				"infoPlus":       true,
			},
		)
	} else {
		content, _ := ioutil.ReadFile(path.Join(GetAppConfig().VhostPath, filename))
		ctx.HTML(http.StatusOK, "siteConfEdit.html",
			gin.H{
				"configFileName": configName,
				"content":        string(content),
				"infoPlus":       false,
			},
		)
	}
}

func DeleteSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]
	os.Remove(filepath.Join(GetAppConfig().VhostPath, filename))
	os.RemoveAll(filepath.Join(GetAppConfig().SSLPath, configName))
	if GetAppConfig().Redis {
		RedisClient.Del(RedisPrefix + configName)
	}
	services.ReloadNginx()
	ctx.Redirect(http.StatusFound, "/sites")
}

func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("filename")
	domains := ctx.PostFormArray("domains[]")
	content := ctx.PostForm("content")
	proxy := ctx.PostForm("proxy")
	if GetAppConfig().Redis {
		SaveSiteDataInRedis(fileName, domains, content, proxy)
	}
	if fileName != "default" {
		fileName = fileName + ".conf"
	}
	//  检测 文件夹是否存在不存在建立
	if _, err := os.Stat(GetAppConfig().VhostPath); os.IsNotExist(err) {
		os.MkdirAll(GetAppConfig().VhostPath, 0755)
	}
	path := filepath.Join(GetAppConfig().VhostPath, fileName)
	ioutil.WriteFile(path, []byte(content), 0644)
	response := services.ReloadNginx()
	ctx.JSON(http.StatusOK, gin.H{"message": response})
}
