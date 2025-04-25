// internal/mqtt/command.go
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"syscall"
	"time"
	"uranus/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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
    Command         string                 `json:"command"`                   // 命令类型
    Params          map[string]interface{} `json:"params,omitempty"`          // 可选参数，支持更复杂的结构
    RequestID       string                 `json:"requestId"`                 // 请求ID，用于匹配响应
    ClientID        string                 `json:"clientId,omitempty"`        // 客户端ID
    Timestamp       int64                  `json:"timestamp,omitempty"`       // 时间戳
    SessionID       string                 `json:"sessionId,omitempty"`       // 终端会话ID
    Streaming       bool                   `json:"streaming,omitempty"`       // 是否流式输出
    TargetRequestID string                 `json:"targetRequestId,omitempty"` // 目标请求ID（用于中断命令）
    Input           string                 `json:"input,omitempty"`           // 终端输入（用于交互式命令）
    Interactive     bool                   `json:"interactive,omitempty"`     // 是否交互式
    SpecialCommand  bool                   `json:"specialCommand,omitempty"`  // 是否为特殊命令(如ping)
    Silent          bool                   `json:"silent,omitempty"`          // 是否静默执行(不输出结果)
    // 添加终端大小相关字段
    Cols            int                    `json:"cols,omitempty"`            // 终端列数
    Rows            int                    `json:"rows,omitempty"`            // 终端行数
    // 添加终端信号相关字段
    Signal          string                 `json:"signal,omitempty"`          // 终端信号类型
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

    // 处理潜在的命令参数结构问题
    fixCommandStructure(&command)

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

    // 终端输入命令的特殊处理
    if command.Command == "terminal_input" && command.SessionID != "" && command.Input != "" {
        log.Printf("[MQTT] 收到终端输入，会话ID: %s", command.SessionID)

        // 处理输入
        success := HandleTerminalInput(command.SessionID, command.Input)

        // 处理结果消息
        message := "处理输入失败"
        if success {
            message = "输入已处理"
        }

        // 如果是Ctrl+C，不发送响应
        if command.Input == "\u0003" {
            return
        }

        // 发送输入处理结果响应
        SendResponse(&ResponseMessage{
            Command:   command.Command,
            RequestID: command.RequestID,
            Success:   success,
            Message:   message,
            SessionID: command.SessionID,
            Timestamp: time.Now().UnixMilli(),
        })
        return
    }

    // 添加终端大小调整命令处理
    if command.Command == "terminal_resize" && command.SessionID != "" {
        log.Printf("[MQTT] 收到终端大小调整命令，会话ID: %s, 行: %d, 列: %d", 
            command.SessionID, command.Rows, command.Cols)
            
        // 如果终端管理器未初始化
        if TerminalMgr == nil {
            SendResponse(&ResponseMessage{
                Command:   command.Command,
                RequestID: command.RequestID,
                Success:   false,
                Message:   "终端管理器未初始化",
                SessionID: command.SessionID,
                Timestamp: time.Now().UnixMilli(),
            })
            return
        }
        
        // 调整终端大小
        err := TerminalMgr.ResizeTerminal(command.SessionID, uint16(command.Rows), uint16(command.Cols))
        
        if err != nil {
            // 调整失败
            SendResponse(&ResponseMessage{
                Command:   command.Command,
                RequestID: command.RequestID,
                Success:   false,
                Message:   fmt.Sprintf("调整终端大小失败: %v", err),
                SessionID: command.SessionID,
                Timestamp: time.Now().UnixMilli(),
            })
            return
        }
        
        // 调整成功
        SendResponse(&ResponseMessage{
            Command:   command.Command,
            RequestID: command.RequestID,
            Success:   true,
            Message:   "终端大小调整成功",
            SessionID: command.SessionID,
            Timestamp: time.Now().UnixMilli(),
        })
        return
    }
    
    // 添加终端信号处理命令
    if command.Command == "terminal_signal" && command.SessionID != "" && command.Signal != "" {
        log.Printf("[MQTT] 收到终端信号命令，会话ID: %s, 信号: %s", 
            command.SessionID, command.Signal)
            
        // 处理逻辑
        success := false
        message := "处理信号失败"
        
        switch command.Signal {
        case "CTRL_C", "SIGINT":
            // 获取进程PID并发送SIGINT
            if requestID, exists := sessionCommands[command.SessionID]; exists {
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
                    term, err := TerminalMgr.GetTerminal(command.SessionID)
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
            if requestID, exists := sessionCommands[command.SessionID]; exists {
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
                term, err := TerminalMgr.GetTerminal(command.SessionID)
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
            if requestID, exists := sessionCommands[command.SessionID]; exists {
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
                term, err := TerminalMgr.GetTerminal(command.SessionID)
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
            message = fmt.Sprintf("不支持的信号类型: %s", command.Signal)
        }
        
        // 发送响应
        SendResponse(&ResponseMessage{
            Command:   command.Command,
            RequestID: command.RequestID,
            Success:   success,
            Message:   message,
            SessionID: command.SessionID,
            Timestamp: time.Now().UnixMilli(),
        })
        return
    }

    // 强制中断命令的特殊处理
    if command.Command == "force_interrupt" && command.SessionID != "" {
        log.Printf("[MQTT] 收到强制中断命令，会话ID: %s", command.SessionID)

        // 记录中断尝试
        interrupted := false

        // 尝试通过会话ID中断
        if InterruptSessionCommand(command.SessionID) {
            interrupted = true
        }

        // 尝试终止与会话关联的所有命令
        activeCommandsLock.RLock()
        for requestID, cmd := range activeCommands {
            if cmd.SessionID == command.SessionID {
                log.Printf("[MQTT] 强制中断会话相关的命令: %s", requestID)
                InterruptCommand(requestID)
                interrupted = true
            }
        }
        activeCommandsLock.RUnlock()

        // 尝试发送系统信号
        terminated := false
        for _, pid := range getProcessesBySession(command.SessionID) {
            log.Printf("[MQTT] 强制终止进程: %d", pid)
            // 如果pid是负数，表示它是进程组ID
            if pid < 0 {
                syscall.Kill(pid, syscall.SIGKILL) // 向进程组发送SIGKILL
            } else {
                syscall.Kill(pid, syscall.SIGKILL) // 向单个进程发送SIGKILL
            }
            terminated = true
        }

        // 发送响应
        SendResponse(&ResponseMessage{
            Command:   command.Command,
            RequestID: command.RequestID,
            Success:   true,
            Message:   fmt.Sprintf("强制中断处理: 中断命令=%v, 终止进程=%v", interrupted, terminated),
            SessionID: command.SessionID,
            Timestamp: time.Now().UnixMilli(),
        })
        return
    }

    // 标准命令处理
    // 查找对应的处理器
    handler, exists := commandHandlers[command.Command]

    // 准备响应
    var response *ResponseMessage

    if exists {
        // 使用对应的处理器处理命令
        response = handler.Handle(&command)
    } else {
        // 如果找不到处理器，尝试使用终端命令处理器处理
        if terminalHandler, ok := commandHandlers["execute"]; ok {
            log.Printf("[MQTT] 尝试将未知命令 '%s' 作为终端命令处理", command.Command)
            response = terminalHandler.Handle(&command)
        } else {
            // 如果终端命令处理器也不存在，返回错误响应
            response = &ResponseMessage{
                Command:   command.Command,
                RequestID: command.RequestID,
                Success:   false,
                Message:   fmt.Sprintf("未知命令: %s", command.Command),
                Timestamp: time.Now().UnixMilli(),
            }
            log.Printf("[MQTT] 未知命令: %s", command.Command)
        }
    }

    // 如果是会话命令，添加会话ID
    if command.SessionID != "" && response != nil {
        response.SessionID = command.SessionID
    }

    // 如果不是静默命令，则发送响应
    if !command.Silent && response != nil {
        // 发送响应
        SendResponse(response)
    }
}

// 在 fixCommandStructure 函数中添加对终端调整大小和终端信号的处理

func fixCommandStructure(cmd *CommandMessage) {
    // 如果命令不是标准命令，且没有参数，尝试修复结构
    standardCommands := map[string]bool{
        "execute": true, "reload": true, "restart": true,
        "stop": true, "start": true, "status": true,
        "update": true, "interrupt": true, "terminal_input": true,
        "force_interrupt": true, "terminal_resize": true, "terminal_signal": true,
    }

    if !standardCommands[cmd.Command] && cmd.Params == nil {
        // 可能是前端直接发送了命令名称，我们修复为execute命令
        actualCommand := cmd.Command
        cmd.Params = make(map[string]interface{})
        cmd.Params["command"] = actualCommand
        cmd.Command = "execute"

        log.Printf("[MQTT] 修复命令结构: '%s' -> 'execute' 命令, 参数command='%s'", actualCommand, actualCommand)
    }

    // 从params中提取终端大小参数
    if cmd.Params != nil {
        // 提取行数
        if rows, ok := cmd.Params["rows"]; ok {
            if rowsInt, ok := rows.(float64); ok {
                cmd.Rows = int(rowsInt)
                delete(cmd.Params, "rows")
            }
        }

        // 提取列数
        if cols, ok := cmd.Params["cols"]; ok {
            if colsInt, ok := cols.(float64); ok {
                cmd.Cols = int(colsInt)
                delete(cmd.Params, "cols")
            }
        }

        // 提取信号类型
        if signal, ok := cmd.Params["signal"]; ok {
            if signalStr, ok := signal.(string); ok {
                cmd.Signal = signalStr
                delete(cmd.Params, "signal")
            }
        }

        // 提取交互式标志
        if interactive, ok := cmd.Params["interactive"]; ok {
            if interactiveBool, ok := interactive.(bool); ok {
                cmd.Interactive = interactiveBool
                delete(cmd.Params, "interactive")
            }
        }

        // 提取特殊命令标志
        if specialCommand, ok := cmd.Params["specialCommand"]; ok {
            if specialBool, ok := specialCommand.(bool); ok {
                cmd.SpecialCommand = specialBool
                delete(cmd.Params, "specialCommand")
            }
        }

        // 提取静默标志
        if silent, ok := cmd.Params["silent"]; ok {
            if silentBool, ok := silent.(bool); ok {
                cmd.Silent = silentBool
                delete(cmd.Params, "silent")
            }
        }
    }
}

// fixCommandStructure 修复命令结构问题，增加与前端的兼容性
func fixCommandStructure(cmd *CommandMessage) {
	// 如果命令不是标准命令，且没有参数，尝试修复结构
	standardCommands := map[string]bool{
		"execute": true, "reload": true, "restart": true,
		"stop": true, "start": true, "status": true,
		"update": true, "interrupt": true, "terminal_input": true,
		"force_interrupt": true,
	}

	if !standardCommands[cmd.Command] && cmd.Params == nil {
		// 可能是前端直接发送了命令名称，我们修复为execute命令
		actualCommand := cmd.Command
		cmd.Params = make(map[string]interface{})
		cmd.Params["command"] = actualCommand
		cmd.Command = "execute"

		log.Printf("[MQTT] 修复命令结构: '%s' -> 'execute' 命令, 参数command='%s'", actualCommand, actualCommand)
	}

	// 从params中提取特殊字段
	if cmd.Params != nil {
		// 提取交互式标志
		if interactive, ok := cmd.Params["interactive"]; ok {
			if interactiveBool, ok := interactive.(bool); ok {
				cmd.Interactive = interactiveBool
				delete(cmd.Params, "interactive")
			}
		}

		// 提取特殊命令标志
		if specialCommand, ok := cmd.Params["specialCommand"]; ok {
			if specialBool, ok := specialCommand.(bool); ok {
				cmd.SpecialCommand = specialBool
				delete(cmd.Params, "specialCommand")
			}
		}

		// 提取静默标志
		if silent, ok := cmd.Params["silent"]; ok {
			if silentBool, ok := silent.(bool); ok {
				cmd.Silent = silentBool
				delete(cmd.Params, "silent")
			}
		}
	}
}

// SendResponse 发送命令响应
func SendResponse(response *ResponseMessage) {
	if response == nil {
		return
	}

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
