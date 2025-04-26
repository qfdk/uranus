package mqtt

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"uranus/internal/services"
)

// 初始化处理器
func init() {
	RegisterHandler("execute", &ExecuteCommandHandler{})
	RegisterHandler("reload", &NginxCommandHandler{command: "reload"})
	RegisterHandler("restart", &NginxCommandHandler{command: "restart"})
	RegisterHandler("stop", &NginxCommandHandler{command: "stop"})
	RegisterHandler("start", &NginxCommandHandler{command: "start"})
	RegisterHandler("status", &StatusCommandHandler{})
	RegisterHandler("update", &UpdateCommandHandler{})
}

// ExecuteCommandHandler 处理execute命令
type ExecuteCommandHandler struct{}

func (h *ExecuteCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	var cmdStr string
	if cmd.Params != nil {
		if cmdParam, ok := cmd.Params["command"]; ok {
			if str, ok := cmdParam.(string); ok {
				cmdStr = str
			}
		}
	}

	if cmdStr == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "未提供执行命令",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	output, err := executeSimpleCommand(cmdStr)

	message := "执行成功"
	if err != nil {
		message = fmt.Sprintf("执行失败: %v", err)
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   err == nil,
		Message:   message,
		Output:    output,
		Timestamp: time.Now().UnixMilli(),
	}
}

// 简化的命令执行函数
func executeSimpleCommand(cmdStr string) (string, error) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", fmt.Errorf("空命令")
	}

	var cmd *exec.Cmd
	if len(parts) > 1 {
		cmd = exec.Command(parts[0], parts[1:]...)
	} else {
		cmd = exec.Command(parts[0])
	}

	// 设置环境和工作目录
	cmd.Env = os.Environ()
	pwd, err := os.Getwd()
	if err == nil {
		cmd.Dir = pwd
	}

	// 执行并获取输出
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// NginxCommandHandler 处理Nginx相关命令
type NginxCommandHandler struct {
	command string
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

	if result != "OK" {
		success = false
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   success,
		Message:   result,
		Timestamp: time.Now().UnixMilli(),
	}
}

// StatusCommandHandler 处理状态命令
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
	}
}

// UpdateCommandHandler 处理更新命令
type UpdateCommandHandler struct{}

func (h *UpdateCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	// 获取更新URL
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
	}
}
