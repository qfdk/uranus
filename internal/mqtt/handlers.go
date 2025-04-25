// internal/mqtt/handlers.go
package mqtt

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"syscall"
	"time"
	"uranus/internal/services"
	"uranus/internal/terminal"
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

	// 注册终端会话处理器
	RegisterHandler("terminal", &TerminalSessionHandler{})

	// 添加终端大小调整命令处理器
	RegisterHandler("terminal_resize", &TerminalResizeHandler{})

	// 添加终端信号命令处理器
	RegisterHandler("terminal_signal", &TerminalSignalHandler{})
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
	var isSpecial bool

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
				interactive = cmd.Interactive // 优先使用顶层字段
				if !interactive && cmd.Params != nil {
					if interactiveParam, ok := cmd.Params["interactive"]; ok {
						if interactiveBool, ok := interactiveParam.(bool); ok {
							interactive = interactiveBool
						}
					}
				}

				// 优先使用传入的特殊命令标志
				isSpecial = cmd.SpecialCommand

				// 如果未指定，尝试自动检测是否为特殊命令
				if !isSpecial {
					isSpecial = isSpecialCommand(commandToExecute)
				}

				// 如果未指定交互式，自动检测
				if !interactive {
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
		interactive = cmd.Interactive
		isSpecial = cmd.SpecialCommand

		// 如果未指定特殊命令标志，尝试自动检测
		if !isSpecial {
			isSpecial = isSpecialCommand(commandToExecute)
		}

		// 如果未指定交互式，尝试自动检测交互式命令
		if !interactive {
			cmdFields := strings.Fields(commandToExecute)
			if len(cmdFields) > 0 {
				cmdName := cmdFields[0]
				switch cmdName {
				case "vim", "vi", "nano", "emacs", "less", "more", "top", "htop":
					interactive = true
				}
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

	// 是否启用安静模式（不返回响应）
	silent := cmd.Silent

	// 记录特殊命令检测结果
	if isSpecial {
		log.Printf("[MQTT] 检测到特殊命令: %s，将使用增强的中断处理", commandToExecute)
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
		if (!streaming || err != nil) && !silent {
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

	// 只有在流式模式下且非静默模式，立即返回确认响应
	if streaming && !silent {
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

	// 非流式模式或静默模式不返回响应，由goroutine处理完成后发送
	return nil
}

// TerminalSessionHandler 处理终端会话相关命令
type TerminalSessionHandler struct{}

func (h *TerminalSessionHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	// 检查终端管理器是否初始化
	if TerminalMgr == nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "终端管理器未初始化",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 解析action参数
	actionParam, ok := cmd.Params["action"]
	if !ok {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少action参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 转换action为字符串
	action, ok := actionParam.(string)
	if !ok {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "action参数类型错误",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 处理不同类型的终端命令
	switch action {
	case "create":
		return h.handleCreateTerminal(cmd)
	case "close":
		return h.handleCloseTerminal(cmd)
	case "resize":
		return h.handleResizeTerminal(cmd)
	case "list":
		return h.handleListTerminals(cmd)
	default:
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "未知的终端命令: " + action,
			Timestamp: time.Now().UnixMilli(),
		}
	}
}

// handleCreateTerminal 处理创建终端命令
func (h *TerminalSessionHandler) handleCreateTerminal(cmd *CommandMessage) *ResponseMessage {
	// 提取shell参数
	var shell string
	if shellParam, ok := cmd.Params["shell"]; ok {
		if shellStr, ok := shellParam.(string); ok {
			shell = shellStr
		}
	}

	// 提取行列参数
	var rows, cols uint16 = 24, 80
	if rowsParam, ok := cmd.Params["rows"]; ok {
		if rowsFloat, ok := rowsParam.(float64); ok && rowsFloat > 0 {
			rows = uint16(rowsFloat)
		}
	}
	if colsParam, ok := cmd.Params["cols"]; ok {
		if colsFloat, ok := colsParam.(float64); ok && colsFloat > 0 {
			cols = uint16(colsFloat)
		}
	}

	// 创建终端会话
	sessionID, err := TerminalMgr.CreateTerminal(shell)
	if err != nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "创建终端失败: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 调整终端大小
	if rows > 0 && cols > 0 {
		term, _ := TerminalMgr.GetTerminal(sessionID)
		if term != nil {
			term.Resize(rows, cols)
		}
	}

	// 返回成功响应
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "终端创建成功",
		Data: map[string]string{
			"sessionId":   sessionID,
			"inputTopic":  "uranus/terminal/" + sessionID + "/input",
			"outputTopic": "uranus/terminal/" + sessionID + "/output",
		},
		Timestamp: time.Now().UnixMilli(),
	}
}

// handleCloseTerminal 处理关闭终端命令
func (h *TerminalSessionHandler) handleCloseTerminal(cmd *CommandMessage) *ResponseMessage {
	// 提取会话ID参数
	sessionIDParam, ok := cmd.Params["sessionId"]
	if !ok {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	sessionID, ok := sessionIDParam.(string)
	if !ok || sessionID == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "无效的会话ID参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 关闭终端会话
	err := TerminalMgr.CloseTerminal(sessionID)
	if err != nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "关闭终端失败: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 返回成功响应
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "终端已关闭",
		Timestamp: time.Now().UnixMilli(),
	}
}

// handleResizeTerminal 处理调整终端大小命令
func (h *TerminalSessionHandler) handleResizeTerminal(cmd *CommandMessage) *ResponseMessage {
	// 提取会话ID参数
	sessionIDParam, ok := cmd.Params["sessionId"]
	if !ok {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	sessionID, ok := sessionIDParam.(string)
	if !ok || sessionID == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "无效的会话ID参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 提取行列参数
	var rows, cols uint16 = 0, 0
	if rowsParam, ok := cmd.Params["rows"]; ok {
		if rowsFloat, ok := rowsParam.(float64); ok && rowsFloat > 0 {
			rows = uint16(rowsFloat)
		}
	}
	if colsParam, ok := cmd.Params["cols"]; ok {
		if colsFloat, ok := colsParam.(float64); ok && colsFloat > 0 {
			cols = uint16(colsFloat)
		}
	}

	if rows == 0 || cols == 0 {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "无效的终端尺寸参数",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 调整终端大小
	err := TerminalMgr.ResizeTerminal(sessionID, rows, cols)
	if err != nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "调整终端大小失败: " + err.Error(),
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 返回成功响应
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "终端大小已调整",
		Timestamp: time.Now().UnixMilli(),
	}
}

// handleListTerminals 处理列出终端命令
func (h *TerminalSessionHandler) handleListTerminals(cmd *CommandMessage) *ResponseMessage {
	// 获取所有会话
	sessions := TerminalMgr.ListSessions()

	// 返回成功响应
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "获取终端列表成功",
		Data:      sessions,
		Timestamp: time.Now().UnixMilli(),
	}
}

// TerminalResizeHandler 处理终端大小调整命令
type TerminalResizeHandler struct{}

func (h *TerminalResizeHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	log.Printf("[MQTT] 正在处理终端大小调整命令，会话ID: %s, 行: %d, 列: %d",
		cmd.SessionID, cmd.Rows, cmd.Cols)

	// 如果终端管理器未初始化
	if TerminalMgr == nil {
		log.Printf("[MQTT] 终端管理器未初始化，无法调整终端大小")
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "终端管理器未初始化",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 方法1: 通过终端管理器调整大小
	err := TerminalMgr.ResizeTerminal(cmd.SessionID, uint16(cmd.Rows), uint16(cmd.Cols))
	if err == nil {
		log.Printf("[MQTT] 通过终端管理器调整终端大小成功")
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   true,
			Message:   "终端大小调整成功",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 方法2: 尝试直接通过会话ID调整大小
	log.Printf("[MQTT] 通过终端管理器调整失败: %v，尝试直接通过会话ID调整", err)
	err = terminal.ResizeBySessionID(cmd.SessionID, uint16(cmd.Rows), uint16(cmd.Cols))
	if err == nil {
		log.Printf("[MQTT] 通过会话ID直接调整终端大小成功")
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   true,
			Message:   "终端大小调整成功（直接访问方式）",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 方法3: 尝试使用 stty 命令（但在macOS上可能会失败）
	log.Printf("[MQTT] 直接调整也失败: %v，尝试使用stty命令", err)

	// 针对macOS返回特殊消息，避免再次尝试
	if runtime.GOOS == "darwin" {
		log.Printf("[MQTT] 在macOS上，终端大小调整可能不可用")
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   true, // 返回成功但提示用户
			Message:   "在macOS上，终端大小调整可能不完全支持",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 非macOS系统尝试使用stty命令
	execCmd := &CommandMessage{
		Command:   "execute",
		RequestID: fmt.Sprintf("resize-stty-%d", time.Now().UnixMilli()),
		SessionID: cmd.SessionID,
		Params: map[string]interface{}{
			"command": fmt.Sprintf("stty rows %d cols %d", cmd.Rows, cmd.Cols),
		},
		Silent: true,
	}

	// 如果找到execute命令处理器，使用它
	if execHandler, ok := commandHandlers["execute"]; ok {
		execHandler.Handle(execCmd)

		// 通知前端已通过替代方案处理
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   true,
			Message:   "通过stty命令调整终端大小",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 如果所有方法都失败
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   false,
		Message:   fmt.Sprintf("终端大小调整失败: %v", err),
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// TerminalSignalHandler 处理终端信号命令
type TerminalSignalHandler struct{}

func (h *TerminalSignalHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	log.Printf("[MQTT] 处理终端信号命令，会话ID: %s, 信号: %s",
		cmd.SessionID, cmd.Signal)

	// 处理逻辑
	success := false
	message := "处理信号失败"

	switch cmd.Signal {
	case "CTRL_C", "SIGINT":
		// 获取进程PID并发送SIGINT
		if requestID, exists := sessionCommands[cmd.SessionID]; exists {
			if activeCmd, exists := activeCommands[requestID]; exists && activeCmd.Cmd != nil && activeCmd.Cmd.Process != nil {
				// 获取进程组ID
				pgid, err := syscall.Getpgid(activeCmd.Cmd.Process.Pid)
				if err == nil {
					// 向进程组发送SIGINT
					err = syscall.Kill(-pgid, syscall.SIGINT)
					if err == nil {
						success = true
						message = "SIGINT信号已发送"
					} else {
						message = fmt.Sprintf("发送SIGINT信号失败: %v", err)
					}
				} else {
					// 如果无法获取进程组ID，直接向进程发送信号
					err = activeCmd.Cmd.Process.Signal(syscall.SIGINT)
					if err == nil {
						success = true
						message = "SIGINT信号已发送到进程"
					} else {
						message = fmt.Sprintf("发送SIGINT信号失败: %v", err)
					}
				}

				// 如果命令是交互式的且使用了伪终端，也向伪终端发送Ctrl+C
				if activeCmd.IsInteractive && activeCmd.Pty != nil {
					_, err := activeCmd.Pty.Write([]byte{3}) // ASCII 3 = Ctrl+C
					if err != nil {
						log.Printf("[MQTT] 向伪终端发送Ctrl+C失败: %v", err)
					}
				}
			}
		} else {
			// 尝试使用终端管理器
			if TerminalMgr != nil {
				term, err := TerminalMgr.GetTerminal(cmd.SessionID)
				if err == nil && term != nil && term.Cmd != nil && term.Cmd.Process != nil {
					pgid, err := syscall.Getpgid(term.Cmd.Process.Pid)
					if err == nil {
						syscall.Kill(-pgid, syscall.SIGINT)
						success = true
						message = "SIGINT信号已通过终端管理器发送"
					} else {
						term.Cmd.Process.Signal(syscall.SIGINT)
						success = true
						message = "SIGINT信号已通过终端管理器发送到进程"
					}
				}
			}
		}

	case "CTRL_D", "EOF":
		// 向伪终端发送EOF
		if requestID, exists := sessionCommands[cmd.SessionID]; exists {
			if activeCmd, exists := activeCommands[requestID]; exists && activeCmd.IsInteractive && activeCmd.Pty != nil {
				_, err := activeCmd.Pty.Write([]byte{4}) // ASCII 4 = Ctrl+D
				if err == nil {
					success = true
					message = "EOF信号已发送"
				} else {
					message = fmt.Sprintf("发送EOF信号失败: %v", err)
				}
			}
		} else if TerminalMgr != nil {
			term, err := TerminalMgr.GetTerminal(cmd.SessionID)
			if err == nil && term != nil && term.Pty != nil {
				_, err := term.Pty.Write([]byte{4})
				if err == nil {
					success = true
					message = "EOF信号已通过终端管理器发送"
				} else {
					message = fmt.Sprintf("发送EOF信号失败: %v", err)
				}
			}
		}

	case "SIGTERM":
		// 发送SIGTERM信号
		if requestID, exists := sessionCommands[cmd.SessionID]; exists {
			if activeCmd, exists := activeCommands[requestID]; exists && activeCmd.Cmd != nil && activeCmd.Cmd.Process != nil {
				pgid, err := syscall.Getpgid(activeCmd.Cmd.Process.Pid)
				if err == nil {
					syscall.Kill(-pgid, syscall.SIGTERM)
					success = true
					message = "SIGTERM信号已发送"
				} else {
					activeCmd.Cmd.Process.Signal(syscall.SIGTERM)
					success = true
					message = "SIGTERM信号已发送到进程"
				}
			}
		} else if TerminalMgr != nil {
			term, err := TerminalMgr.GetTerminal(cmd.SessionID)
			if err == nil && term != nil && term.Cmd != nil && term.Cmd.Process != nil {
				pgid, err := syscall.Getpgid(term.Cmd.Process.Pid)
				if err == nil {
					syscall.Kill(-pgid, syscall.SIGTERM)
					success = true
					message = "SIGTERM信号已通过终端管理器发送"
				} else {
					term.Cmd.Process.Signal(syscall.SIGTERM)
					success = true
					message = "SIGTERM信号已通过终端管理器发送到进程"
				}
			}
		}

	default:
		message = fmt.Sprintf("不支持的信号类型: %s", cmd.Signal)
	}

	// 发送响应
	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   success,
		Message:   message,
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}
