package controllers

import (
	_ "embed"
	"encoding/json"
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

//go:embed template/http.conf
var httpConf string

//go:embed template/https.conf
var httpsConf string

// GetConfig  TODO /** 安全问题 ！！！跨目录
func GetConfig(ctx *gin.Context) {
	name := ctx.Query("name")
	path := filepath.Join(services.GetNginxConfPath(), name)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "siteEdit.html", gin.H{"configFileName": name, "content": string(content)})
}

func NewSite(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "siteEdit.html", gin.H{"configFileName": "", "content": ""})
}

func GetTemplate(ctx *gin.Context) {
	domains := ctx.QueryArray("domains[]")
	configName := ctx.Query("configName")
	enableSSL, _ := strconv.ParseBool(ctx.Query("ssl"))
	var templateConf = httpConf
	if enableSSL {
		templateConf = httpsConf
	}
	inputTemplate := templateConf
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{domain}}", strings.Join(domains[:], " "))
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{configName}}", configName)
	inputTemplate = strings.ReplaceAll(inputTemplate, "{{sslPath}}", config.GetAppConfig().SSLPath)
	ctx.JSON(http.StatusOK, gin.H{"content": inputTemplate})
}

func GetSites(ctx *gin.Context) {
	files, err := ioutil.ReadDir(filepath.Join(config.GetAppConfig().VhostPath))
	if err != nil {
		fmt.Println(err)
		ctx.HTML(http.StatusOK, "sites.html", gin.H{"files": []string{}})
		return
	}
	ctx.HTML(http.StatusOK, "sites.html", gin.H{"files": files, "humanizeBytes": humanize.Bytes})
}

func EditSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]
	redisData, _ := config.GetRedisClient().Get("nginx:" + configName).Result()
	var output gin.H
	json.Unmarshal([]byte(redisData), &output)
	ctx.HTML(http.StatusOK, "edit.html",
		gin.H{
			"configFileName": output["fileName"],
			"domains":        output["domains"],
			"content":        output["content"],
		},
	)
}

func DeleteSiteConf(ctx *gin.Context) {
	filename := ctx.Param("filename")
	configName := strings.Split(filename, ".conf")[0]
	path := filepath.Join(config.GetAppConfig().VhostPath, filename)
	os.Remove(path)
	config.GetRedisClient().Del("nginx:" + configName)
	services.ReloadNginx()
	ctx.Redirect(http.StatusFound, "/sites")
}

func saveDataInRedis(fileName string, domains []string, content string) {
	data := make(gin.H)
	data["fileName"] = fileName
	data["domains"] = strings.Join(domains[:], ",")
	data["content"] = content
	res, _ := json.Marshal(data)
	err := config.GetRedisClient().Set("nginx:"+fileName, res, 0).Err()
	if err != nil {
		fmt.Println(err)
	}
}

func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("filename")
	domains := ctx.PostFormArray("domains[]")
	content := ctx.PostForm("content")
	saveDataInRedis(fileName, domains, content)
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
