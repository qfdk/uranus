// internal/mqtt/handlers.go
package mqtt

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"
	"uranus/internal/services"
)

// 初始化函数，注册所有命令处理器
func init() {
	// 注册Nginx相关命令处理器 - 使用统一的命令名称
	RegisterHandler("reload", &NginxCommandHandler{command: "reload"})
	RegisterHandler("restart", &NginxCommandHandler{command: "restart"})
	RegisterHandler("stop", &NginxCommandHandler{command: "stop"})
	RegisterHandler("start", &NginxCommandHandler{command: "start"})

	// 注册状态命令处理器
	RegisterHandler("status", &StatusCommandHandler{})

	// 注册更新命令处理器
	RegisterHandler("update", &UpdateCommandHandler{})

	// 注册终端命令处理器
	RegisterHandler("execute", &TerminalCommandHandler{})
}

// NginxCommandHandler 处理Nginx相关命令
type NginxCommandHandler struct {
	command string // 实际命令: reload, restart, stop, start
}

func (h *NginxCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	var result string
	var success bool = true

	switch h.command {
	case "reload":
		result = services.ReloadNginx()
	case "restart":
		stopResult := services.StopNginx()
		if stopResult == "OK" {
			result = services.StartNginx()
		} else {
			result = stopResult
		}
	case "stop":
		result = services.StopNginx()
	case "start":
		result = services.StartNginx()
	}

	// 如果返回结果不是"OK"，则认为操作失败
	if result != "OK" {
		success = false
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   success,
		Message:   result,
		Timestamp: time.Now().UnixMilli(),
		SessionID: cmd.SessionID, // 保留会话ID以支持终端操作
	}
}

// StatusCommandHandler 处理状态查询命令
type StatusCommandHandler struct{}

func (h *StatusCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	nginxStatus := services.NginxStatus()

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   nginxStatus,
		Data: map[string]interface{}{
			"nginx": nginxStatus != "KO",
		},
		Timestamp: time.Now().UnixMilli(),
		SessionID: cmd.SessionID,
	}
}

// UpdateCommandHandler 处理系统更新命令
type UpdateCommandHandler struct{}

func (h *UpdateCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	// 获取更新URL，如果命令中提供了URL则使用，否则使用默认URL
	updateUrl := "https://fr.qfdk.me/uranus/uranus-" + runtime.GOARCH
	if params, ok := cmd.Params["url"]; ok {
		if url, ok := params.(string); ok && url != "" {
			updateUrl = url
		}
	}

	// 异步执行更新
	go func() {
		err := services.ToUpdateProgram(updateUrl)
		if err != nil {
			log.Printf("[MQTT] 更新失败: %v", err)
		}
	}()

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "更新操作已开始执行",
		Timestamp: time.Now().UnixMilli(),
		SessionID: cmd.SessionID,
	}
}

// TerminalCommandHandler 处理终端命令执行
type TerminalCommandHandler struct{}

func (h *TerminalCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	// 先准备一个基础响应
	response := &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Timestamp: time.Now().UnixMilli(),
		SessionID: cmd.SessionID,
	}

	// 获取执行的命令
	var commandToExecute string
	var streaming bool
	var interactive bool

	// 命令参数处理：兼容两种不同的消息结构
	if cmd.Params != nil {
		// 正常情况：从params中获取command
		if cmdParam, ok := cmd.Params["command"]; ok {
			if cmdStr, ok := cmdParam.(string); ok && cmdStr != "" {
				commandToExecute = cmdStr

				// 判断是否为流式执行
				if streamParam, ok := cmd.Params["streaming"]; ok {
					if streamBool, ok := streamParam.(bool); ok {
						streaming = streamBool
					}
				} else {
					streaming = cmd.Streaming // 从顶层获取
				}

				// 判断是否为交互式命令
				if interactiveParam, ok := cmd.Params["interactive"]; ok {
					if interactiveBool, ok := interactiveParam.(bool); ok {
						interactive = interactiveBool
					}
				} else {
					// 尝试自动检测交互式命令
					cmdFields := strings.Fields(commandToExecute)
					if len(cmdFields) > 0 {
						cmdName := cmdFields[0]
						switch cmdName {
						case "vim", "vi", "nano", "emacs", "less", "more", "top", "htop":
							interactive = true
						}
					}
				}
			} else {
				response.Success = false
				response.Message = "无效的命令格式"
				log.Printf("[MQTT] 无效的命令格式")
				return response
			}
		} else {
			response.Success = false
			response.Message = "未提供execute命令"
			log.Printf("[MQTT] 未提供execute命令")
			return response
		}
	} else if cmd.Command != "execute" {
		// 兼容性处理：如果没有params且命令不是"execute"
		// 可能是前端直接发送了命令名称作为Command
		commandToExecute = cmd.Command
		streaming = cmd.Streaming
		interactive = false // 默认非交互式

		// 尝试自动检测交互式命令
		cmdFields := strings.Fields(commandToExecute)
		if len(cmdFields) > 0 {
			cmdName := cmdFields[0]
			switch cmdName {
			case "vim", "vi", "nano", "emacs", "less", "more", "top", "htop":
				interactive = true
			}
		}

		// 修改响应中的命令名称为"execute"，保持一致性
		response.Command = "execute"
	} else {
		response.Success = false
		response.Message = "未提供execute命令"
		log.Printf("[MQTT] 未提供execute命令")
		return response
	}

	// 检查是否获取到了有效的命令
	if commandToExecute == "" {
		response.Success = false
		response.Message = "无效的命令"
		log.Printf("[MQTT] 无效的命令")
		return response
	}

	// 使用终端处理程序执行命令
	go func() {
		output, err := executeTerminalCommand(
			commandToExecute,
			cmd.SessionID,
			cmd.RequestID,
			streaming,
			interactive,
		)

		// 如果不是流式输出，或者发生错误，发送完整响应
		if !streaming || err != nil {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
				response.Success = false
				response.Message = fmt.Sprintf("命令执行失败: %v", err)
			} else {
				response.Success = true
				response.Message = "命令执行成功"
			}

			response.Output = output
			if response.Output == "" && errMsg == "" {
				response.Output = "命令执行成功，无输出"
			}

			// 如果是流式输出的错误情况，设置为最终响应
			if streaming {
				response.Streaming = true
				response.Final = true
			}

			SendResponse(response)
		}
	}()

	// 只有在流式模式下，立即返回确认响应
	if streaming {
		return &ResponseMessage{
			Command:   "execute", // 始终使用"execute"作为命令名称
			RequestID: cmd.RequestID,
			Success:   true,
			Message:   "",
			SessionID: cmd.SessionID,
			Streaming: true,
			Final:     false,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 非流式模式不返回响应，由goroutine处理完成后发送
	return nil
}
