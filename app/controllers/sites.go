package controllers

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"proxy-manager/app/services"
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
	ctx.HTML(http.StatusOK, "edit", gin.H{"configFileName": name, "content": string(content)})
}

func GetSites(ctx *gin.Context) {
	files, err := ioutil.ReadDir(filepath.Join(services.GetNginxConfPath(), "servers"))
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "sites", gin.H{"files": files, "humanizeBytes": humanize.Bytes})
}

func EditSiteConf(ctx *gin.Context) {
	name := ctx.Query("path")
	path := filepath.Join(services.GetNginxConfPath(), "servers", name)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	ctx.HTML(http.StatusOK, "edit", gin.H{"configFileName": name, "content": string(content)})
}

func DeleteSiteConf(ctx *gin.Context) {
	name := ctx.Query("path")
	path := filepath.Join(services.GetNginxConfPath(), "servers", name)
	os.Remove(path)
	ctx.Redirect(http.StatusFound, "/sites")
}

func SaveSiteConf(ctx *gin.Context) {
	fileName := ctx.PostForm("name")
	content := ctx.PostForm("content")
	path := filepath.Join(services.GetNginxConfPath(), "servers", fileName)
	ioutil.WriteFile(path, []byte(content), 0644)
	ctx.JSON(http.StatusOK, gin.H{"msg": "OK"})
}
