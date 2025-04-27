package controllers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log"
	"net/http"
	"uranus/internal/config"
)

// MQTTTerminalStart 启动MQTT终端页面
func MQTTTerminalStart(ctx *gin.Context) {
	// 生成会话ID
	sessionID := uuid.New().String()

	// 获取配置
	appConfig := config.GetAppConfig()

	// 渲染终端页面
	ctx.HTML(http.StatusOK, "mqtty.html", gin.H{
		"activePage": "terminal",
		"sessionID":  sessionID,
		"agentID":    appConfig.UUID,
		"mqttBroker": appConfig.MQTTBroker,
	})
}

// MQTTTerminalList 列出终端会话
func MQTTTerminalList(ctx *gin.Context) {
	// 此功能需要进一步实现，需要查询会话列表
	// 目前只返回空列表
	ctx.JSON(http.StatusOK, gin.H{
		"sessions": []string{},
	})
}

// MQTTTerminalClose 关闭终端会话
func MQTTTerminalClose(ctx *gin.Context) {
	sessionID := ctx.Param("sessionID")

	// 此功能需要进一步实现，向MQTT发送关闭命令
	log.Printf("尝试关闭会话: %s", sessionID)

	ctx.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "关闭命令已发送",
	})
}
