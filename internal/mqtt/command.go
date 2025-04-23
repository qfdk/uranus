// internal/mqtt/command.go
package mqtt

import (
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"time"
	"uranus/internal/config"
)

const (
	// MQTT主题定义
	HeartbeatTopic = "uranus/heartbeat" // 心跳消息主题
	CommandTopic   = "uranus/command/"  // 命令主题前缀，会拼接UUID
	ResponseTopic  = "uranus/response/" // 响应主题前缀，会拼接UUID
	StatusTopic    = "uranus/status"    // 全局状态主题
)

// CommandMessage 命令消息结构
type CommandMessage struct {
	Command         string                 `json:"command"`                   // 命令类型: reload, restart, stop, execute等
	Params          map[string]interface{} `json:"params,omitempty"`          // 可选参数，支持更复杂的结构
	RequestID       string                 `json:"requestId"`                 // 请求ID，用于匹配响应
	ClientID        string                 `json:"clientId,omitempty"`        // 客户端ID
	Timestamp       int64                  `json:"timestamp,omitempty"`       // 时间戳
	SessionID       string                 `json:"sessionId,omitempty"`       // 终端会话ID
	Streaming       bool                   `json:"streaming,omitempty"`       // 是否流式输出
	TargetRequestID string                 `json:"targetRequestId,omitempty"` // 目标请求ID（用于中断命令）
}

// ResponseMessage 响应消息结构
type ResponseMessage struct {
	Command   string      `json:"command"`             // 对应的命令
	RequestID string      `json:"requestId"`           // 对应的请求ID
	Success   bool        `json:"success"`             // 是否成功
	Message   string      `json:"message,omitempty"`   // 响应消息
	Output    string      `json:"output,omitempty"`    // 命令输出
	Data      interface{} `json:"data,omitempty"`      // 可选的返回数据
	Timestamp int64       `json:"timestamp"`           // 时间戳
	SessionID string      `json:"sessionId,omitempty"` // 会话ID
	Streaming bool        `json:"streaming,omitempty"` // 是否为流式输出
	Final     bool        `json:"final,omitempty"`     // 是否为最终响应（流式输出的结束）
}

// CommandHandler 命令处理器接口
type CommandHandler interface {
	Handle(cmd *CommandMessage) *ResponseMessage
}

// 命令处理器映射表
var commandHandlers = make(map[string]CommandHandler)

// RegisterHandler 注册命令处理器
func RegisterHandler(command string, handler CommandHandler) {
	commandHandlers[command] = handler
}

// handleCommand 处理接收到的命令
func handleCommand(client mqtt.Client, msg mqtt.Message) {
	log.Printf("[MQTT] 收到命令: %s", string(msg.Payload()))

	// 解析命令
	var command CommandMessage
	if err := json.Unmarshal(msg.Payload(), &command); err != nil {
		log.Printf("[MQTT] 命令解析失败: %v", err)
		return
	}

	// 中断命令的特殊处理
	if command.Command == "interrupt" && command.TargetRequestID != "" {
		log.Printf("[MQTT] 收到中断命令，目标请求ID: %s", command.TargetRequestID)
		if InterruptCommand(command.TargetRequestID) {
			// 发送中断成功响应
			SendResponse(&ResponseMessage{
				Command:   command.Command,
				RequestID: command.RequestID,
				Success:   true,
				Message:   "命令已中断",
				SessionID: command.SessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		} else {
			// 发送中断失败响应
			SendResponse(&ResponseMessage{
				Command:   command.Command,
				RequestID: command.RequestID,
				Success:   false,
				Message:   "命令未找到或已完成",
				SessionID: command.SessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		}
		return
	}

	// 查找对应的处理器
	handler, exists := commandHandlers[command.Command]

	// 准备响应
	var response *ResponseMessage

	if exists {
		// 使用对应的处理器处理命令
		response = handler.Handle(&command)
	} else {
		// 如果找不到处理器，返回错误响应
		response = &ResponseMessage{
			Command:   command.Command,
			RequestID: command.RequestID,
			Success:   false,
			Message:   fmt.Sprintf("未知命令: %s", command.Command),
			Timestamp: time.Now().UnixMilli(),
		}
		log.Printf("[MQTT] 未知命令: %s", command.Command)
	}

	// 如果是会话命令，添加会话ID
	if command.SessionID != "" && response != nil {
		response.SessionID = command.SessionID
	}

	// 发送响应
	SendResponse(response)
}

// SendResponse 发送命令响应
func SendResponse(response *ResponseMessage) {
	appConfig := config.GetAppConfig()
	responseTopic := ResponseTopic + appConfig.UUID

	payload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTT] 响应序列化失败: %v", err)
		return
	}

	// 简化日志输出，避免过多输出
	if !response.Streaming || response.Final {
		log.Printf("[MQTT] 发送响应到 %s: %+v", responseTopic, response)
	}

	err = Publish(responseTopic, payload)
	if err != nil {
		log.Printf("[MQTT] 响应发送失败: %v", err)
	}
}
