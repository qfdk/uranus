// internal/mqtt/main.go
package mqtt

import (
	"context"
	"encoding/json"
	"log"
	"uranus/internal/config"
)

// Init 初始化MQTT模块并启动心跳服务
func Init(ctx context.Context) {
	log.Println("[MQTT] 模块初始化")

	// 初始化MQTT连接
	if err := InitMQTT(); err != nil {
		log.Printf("[MQTT] 初始化失败: %v", err)
		return
	}

	// 初始化终端会话管理器
	InitTerminalSessionManager()
	log.Println("[MQTT] 终端管理器已初始化")

	// 注册终端命令处理器
	RegisterHandler("terminal", &TerminalCommandHandler{})
	log.Println("[MQTT] 终端命令处理器已注册")

	// 启动MQTT心跳服务
	go StartHeartbeat(ctx)
}

// TerminalCommandHandler 终端命令处理器
type TerminalCommandHandler struct{}

func (h *TerminalCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	log.Printf("[MQTT] 处理终端命令: %+v", cmd)

	// 获取UUID
	appConfig := config.GetAppConfig()

	// 确保命令类型存在于Params中
	if cmd.Params == nil {
		cmd.Params = make(map[string]interface{})
	}

	// 如果Params中没有type字段，需要添加
	if _, hasType := cmd.Params["type"]; !hasType {
		// 默认使用create类型
		cmd.Params["type"] = "create"
	}

	// 确保会话ID存在
	if _, hasSession := cmd.Params["sessionId"]; !hasSession && cmd.SessionID != "" {
		cmd.Params["sessionId"] = cmd.SessionID
	}

	// 将命令参数转为JSON字节
	paramBytes, err := json.Marshal(cmd.Params)
	if err != nil {
		log.Printf("[MQTT] 序列化终端命令参数失败: %v", err)
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "序列化终端命令参数失败: " + err.Error(),
			Timestamp: GetTimestamp(),
		}
	}

	// 委托给专门的终端处理器
	HandleTerminalCommand(cmd.ClientID, cmd.RequestID, appConfig.UUID, paramBytes)

	// 这里不返回响应，因为在HandleTerminalCommand中会发送响应
	return nil
}
