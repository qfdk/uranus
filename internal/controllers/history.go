package controllers

import (
	"net/http"
	"strconv"
	"uranus/internal/models"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
)

// ConfigHistory lists the earlier versions of a configuration file, newest first.
func ConfigHistory(ctx *gin.Context) {
	versions := []gin.H{}
	for _, v := range models.ConfigVersions(ctx.Query("path")) {
		versions = append(versions, gin.H{
			"id":   v.ID,
			"time": v.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			"size": humanize.Bytes(uint64(len(v.Content))),
		})
	}
	ctx.JSON(http.StatusOK, gin.H{"versions": versions})
}

// ConfigVersionContent returns the content of one earlier version.
func ConfigVersionContent(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 0)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"message": "没有这个版本"})
		return
	}
	version, ok := models.FindConfigVersion(uint(id))
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"message": "没有这个版本"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"path":    version.Path,
		"time":    version.CreatedAt.Local().Format("2006-01-02 15:04:05"),
		"content": version.Content,
	})
}
