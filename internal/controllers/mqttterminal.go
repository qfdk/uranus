package controllers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	"uranus/internal/config"
	"uranus/internal/mqtty"

	"github.com/gin-gonic/gin"
)

// MQTTTerminalInfo 包含MQTT终端连接所需的信息
type MQTTTerminalInfo struct {
	AgentUUID   string `json:"agentUUID"`
	MQTTBroker  string `json:"mqttBroker"`
	TopicPrefix string `json:"topicPrefix"`
	SessionID   string `json:"sessionID"`
	Available   bool   `json:"available"`
}

// CheckAgentAvailability 检查代理是否在线
func CheckAgentAvailability(agentUUID string) bool {
	// 如果是本地代理，直接返回true
	appConfig := config.GetAppConfig()
	if appConfig.UUID == agentUUID {
		return true
	}

	// 这里应该有更复杂的逻辑来检查远程代理是否在线
	// 可以通过MQTT状态主题或其他方式来判断
	// 简单起见，我们先返回true
	return true
}

// MQTTTerminalConnect 处理MQTT终端连接请求
func MQTTTerminalConnect(c *gin.Context) {
	// 获取请求的代理UUID
	agentUUID := c.Query("agent")
	if agentUUID == "" {
		// 如果未指定代理，使用本地代理
		agentUUID = config.GetAppConfig().UUID
	}

	// 检查代理是否可用
	available := CheckAgentAvailability(agentUUID)

	// 生成会话ID
	sessionID := fmt.Sprintf("mqtty-%d", time.Now().UnixNano())

	// 如果代理可用，预创建MQTT会话
	if available {
		// 获取MQTT代理地址
		mqttBroker := config.GetAppConfig().MQTTBroker
		if mqttBroker == "" {
			mqttBroker = "mqtt://mqtt.qfdk.me:1883" // 默认MQTT服务器地址
		}

		// 如果是本地代理且MQTT会话管理器可用，预创建会话
		if agentUUID == config.GetAppConfig().UUID && mqtty.GetGlobalSessionManager() != nil {
			// 预创建本地会话 - 这仅仅是为了提前准备，实际会话会由MQTT消息处理器创建
			log.Printf("[MQTT Terminal] Pre-initializing local session: %s", sessionID)
		}
	}

	// 返回MQTT终端连接信息
	terminalInfo := MQTTTerminalInfo{
		AgentUUID:   agentUUID,
		MQTTBroker:  config.GetAppConfig().MQTTBroker,
		TopicPrefix: "uranus/terminal",
		SessionID:   sessionID,
		Available:   available,
	}

	c.JSON(http.StatusOK, terminalInfo)
}

// MQTTTerminalPage 提供MQTT终端页面
func MQTTTerminalPage(c *gin.Context) {
	// 获取请求的代理UUID
	agentUUID := c.Query("agent")
	if agentUUID == "" {
		// 如果未指定代理，使用本地代理
		agentUUID = config.GetAppConfig().UUID
	}
	
	c.HTML(http.StatusOK, "terminal.html", gin.H{
		"title":    "MQTT Terminal",
		"agentUUID": agentUUID,
		"mode":     "mqtt",
	})
}

// SendMQTTTerminalCommand 发送命令到MQTT终端
func SendMQTTTerminalCommand(c *gin.Context) {
	var command struct {
		AgentUUID string      `json:"agentUUID"`
		Type      string      `json:"type"`
		SessionID string      `json:"sessionID"`
		Data      interface{} `json:"data"`
		RequestID string      `json:"requestId"`
	}

	if err := c.BindJSON(&command); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// 验证必要字段
	if command.AgentUUID == "" || command.SessionID == "" || command.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required fields"})
		return
	}

	// 获取MQTT客户端
	mqttClient := mqtty.GetMQTTClient()
	if mqttClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT client not available"})
		return
	}

	// 检查客户端是否连接
	connected := false
	if mqttClient != nil {
		// 使用接口类型断言来获取IsConnected方法
		if client, ok := mqttClient.(interface{ IsConnected() bool }); ok {
			connected = client.IsConnected()
		}
	}

	if !connected {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MQTT client not connected"})
		return
	}

	// 构建命令消息
	commandMsg := map[string]interface{}{
		"command":   "terminal",
		"type":      command.Type,
		"sessionId": command.SessionID,
		"data":      command.Data,
		"requestId": command.RequestID,
		"clientId":  config.GetAppConfig().UUID,
	}

	// 序列化命令
	commandBytes, err := json.Marshal(commandMsg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to serialize command"})
		return
	}

	// 发送到命令主题
	commandTopic := fmt.Sprintf("uranus/command/%s", command.AgentUUID)
	
	// 直接使用MQTT客户端发布消息
	publishResult := false
	var publishError error
	
	if mqttClient != nil {
		token := mqttClient.Publish(commandTopic, 1, false, commandBytes)
		if token.Wait() && token.Error() != nil {
			publishError = token.Error()
		} else {
			publishResult = true
		}
	}
	
	if !publishResult {
		errMsg := "Failed to send command"
		if publishError != nil {
			errMsg += ": " + publishError.Error()
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Command sent successfully"})
}