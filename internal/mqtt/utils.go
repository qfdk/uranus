// internal/mqtt/utils.go
package mqtt

import (
	"encoding/json"
	"log"
	"time"
	"uranus/internal/config"
)

// GetTimestamp 获取当前时间戳（毫秒）
func GetTimestamp() int64 {
	return time.Now().UnixMilli()
}

// PublishToResponseTopic 发布消息到响应主题
func PublishToResponseTopic(agentUUID string, payload []byte) error {
	responseTopic := ResponseTopic + agentUUID
	return Publish(responseTopic, payload)
}

// SendErrorResponse 发送错误响应
func SendErrorResponse(agentUUID string, requestID string, message string, err error) {
	// 构建错误消息
	errMsg := message
	if err != nil {
		errMsg = message + ": " + err.Error()
	}

	response := ResponseMessage{
		Command:   "terminal",
		RequestID: requestID,
		Success:   false,
		Message:   errMsg,
		Timestamp: GetTimestamp(),
	}

	// 序列化响应
	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTT] 序列化错误响应失败: %v", err)
		return
	}

	// 发布响应
	if err := PublishToResponseTopic(agentUUID, responseBytes); err != nil {
		log.Printf("[MQTT] 发布错误响应失败: %v", err)
	}
}

// SendStatusResponse 发送状态响应
func SendStatusResponse(agentUUID string, sessionID string, statusType string, data interface{}) {
	// 构建状态消息
	statusMsg := map[string]interface{}{
		"sessionId": sessionID,
		"type":      "status",
		"data":      statusType,
		"timestamp": GetTimestamp(),
	}

	// 如果有附加数据，添加到消息中
	if data != nil {
		statusMsg["message"] = data
	}

	// 序列化状态消息
	statusBytes, err := json.Marshal(statusMsg)
	if err != nil {
		log.Printf("[MQTT] 序列化状态消息失败: %v", err)
		return
	}

	// 发布状态消息
	if err := PublishToResponseTopic(agentUUID, statusBytes); err != nil {
		log.Printf("[MQTT] 发布状态消息失败: %v", err)
	}
}

// SendTerminalResponse 发送终端响应
func SendTerminalResponse(agentUUID string, sessionID string, data string) {
	// 获取UUID
	appConfig := config.GetAppConfig()

	// 构建输出消息
	outputMsg := map[string]interface{}{
		"sessionId": sessionID,
		"type":      "output",
		"data":      data,
		"timestamp": GetTimestamp(),
	}

	// 序列化消息
	outputBytes, err := json.Marshal(outputMsg)
	if err != nil {
		log.Printf("[MQTT] 序列化输出消息失败: %v", err)
		return
	}

	// 发布输出消息
	PublishToResponseTopic(appConfig.UUID, outputBytes)
}
