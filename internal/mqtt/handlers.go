package mqtt

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"uranus/internal/services"

	"github.com/creack/pty"
)

// 全局会话管理
var (
	sessionPTY         = make(map[string]*os.File)
	sessionPTYLock     sync.RWMutex
	sessionProcesses   = make(map[string]int)
	sessionProcessLock sync.RWMutex
	sessionCancels     = make(map[string]context.CancelFunc)
	sessionCancelLock  sync.RWMutex
)

// 初始化处理器
func init() {
	RegisterHandler("execute", &ExecuteCommandHandler{})
	RegisterHandler("interactiveShell", &InteractiveShellHandler{})
	RegisterHandler("terminal_input", &TerminalInputHandler{})
	RegisterHandler("terminal_resize", &TerminalResizeHandler{})
	RegisterHandler("closeTerminal", &CloseTerminalHandler{})
	RegisterHandler("reload", &NginxCommandHandler{command: "reload"})
	RegisterHandler("restart", &NginxCommandHandler{command: "restart"})
	RegisterHandler("stop", &NginxCommandHandler{command: "stop"})
	RegisterHandler("start", &NginxCommandHandler{command: "start"})
	RegisterHandler("status", &StatusCommandHandler{})
	RegisterHandler("update", &UpdateCommandHandler{})
}

// ExecuteCommandHandler 处理execute命令(兼容旧接口)
type ExecuteCommandHandler struct{}

func (h *ExecuteCommandHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	log.Printf("[MQTT] 执行命令: %v", cmd)

	// 获取执行命令
	var cmdStr string
	if cmd.Params != nil {
		if cmdParam, ok := cmd.Params["command"]; ok {
			if str, ok := cmdParam.(string); ok {
				cmdStr = str
			}
		}
	}

	// 检查命令
	if cmdStr == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "未提供执行命令",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 存储取消函数
	if cmd.SessionID != "" {
		sessionCancelLock.Lock()
		sessionCancels[cmd.SessionID] = cancel
		sessionCancelLock.Unlock()
	}

	// 执行命令
	go func() {
		output, err := executeCommand(cmdStr, cmd.SessionID, cmd.RequestID, ctx)
		if err != nil {
			log.Printf("[MQTT] 执行命令失败: %v", err)
			SendResponse(&ResponseMessage{
				Command:   cmd.Command,
				RequestID: cmd.RequestID,
				Success:   false,
				Message:   fmt.Sprintf("执行失败: %v", err),
				Output:    output,
				SessionID: cmd.SessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		} else if !cmd.Streaming {
			SendResponse(&ResponseMessage{
				Command:   cmd.Command,
				RequestID: cmd.RequestID,
				Success:   true,
				Message:   "执行成功",
				Output:    output,
				SessionID: cmd.SessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		}
	}()

	// 流式模式返回确认消息
	if cmd.Streaming {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   true,
			Message:   "命令执行中",
			Streaming: true,
			Final:     false,
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	return nil
}

// InteractiveShellHandler 处理交互式shell
type InteractiveShellHandler struct{}

func (h *InteractiveShellHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	log.Printf("[MQTT] 创建交互式会话: %s", cmd.SessionID)

	// 检查会话ID
	if cmd.SessionID == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 检查会话是否存在
	sessionPTYLock.RLock()
	_, exists := sessionPTY[cmd.SessionID]
	sessionPTYLock.RUnlock()

	if exists {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "会话已存在",
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 保存取消函数
	sessionCancelLock.Lock()
	sessionCancels[cmd.SessionID] = cancel
	sessionCancelLock.Unlock()

	// 创建shell命令
	shell := findDefaultShell()
	shellCmd := exec.CommandContext(ctx, shell)
	shellCmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// 设置进程属性
	if runtime.GOOS == "darwin" {
		shellCmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
	} else {
		shellCmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Setsid:  true,
		}
	}

	// 创建伪终端
	ptmx, err := pty.Start(shellCmd)
	if err != nil {
		cancel()
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   fmt.Sprintf("创建伪终端失败: %v", err),
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 保存PTY和进程ID
	sessionPTYLock.Lock()
	sessionPTY[cmd.SessionID] = ptmx
	sessionPTYLock.Unlock()

	sessionProcessLock.Lock()
	sessionProcesses[cmd.SessionID] = shellCmd.Process.Pid
	sessionProcessLock.Unlock()

	// 设置终端大小
	rows, cols := uint16(24), uint16(80)
	if cmd.Rows > 0 {
		rows = uint16(cmd.Rows)
	}
	if cmd.Cols > 0 {
		cols = uint16(cmd.Cols)
	}

	pty.Setsize(ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})

	// 启动输出处理
	go handleTerminalOutput(cmd.SessionID, ptmx)

	// 启动进程监控
	go monitorShellProcess(cmd.SessionID, shellCmd)

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   "交互式会话已创建",
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// TerminalInputHandler 处理终端输入
type TerminalInputHandler struct{}

func (h *TerminalInputHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	if cmd.SessionID == "" || cmd.Input == "" {
		return nil
	}

	// 处理输入
	handleTerminalInput(cmd.SessionID, cmd.Input)
	return nil
}

// TerminalResizeHandler 处理终端大小调整
type TerminalResizeHandler struct{}

func (h *TerminalResizeHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	if cmd.SessionID == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 调整终端大小
	rows := uint16(24)
	cols := uint16(80)

	if cmd.Rows > 0 {
		rows = uint16(cmd.Rows)
	}
	if cmd.Cols > 0 {
		cols = uint16(cmd.Cols)
	}

	success := resizeTerminal(cmd.SessionID, rows, cols)
	message := "调整终端大小失败"
	if success {
		message = "终端大小已调整"
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   success,
		Message:   message,
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// CloseTerminalHandler 处理终端关闭
type CloseTerminalHandler struct{}

func (h *CloseTerminalHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	if cmd.SessionID == "" {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 关闭会话
	success := closeTerminalSession(cmd.SessionID)
	message := "关闭终端会话失败"
	if success {
		message = "终端会话已关闭"
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   success,
		Message:   message,
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// 辅助函数 - 处理终端输入
func handleTerminalInput(sessionID string, input string) bool {
	sessionPTYLock.RLock()
	ptmx, exists := sessionPTY[sessionID]
	sessionPTYLock.RUnlock()

	if !exists || ptmx == nil {
		return false
	}

	// 特殊处理Ctrl+C
	if input == "\u0003" {
		sessionProcessLock.RLock()
		pid, hasPid := sessionProcesses[sessionID]
		sessionProcessLock.RUnlock()

		if hasPid {
			pgid, err := syscall.Getpgid(pid)
			if err == nil && pgid > 0 && runtime.GOOS != "darwin" {
				syscall.Kill(-pgid, syscall.SIGINT)
			} else {
				syscall.Kill(pid, syscall.SIGINT)
			}
		}

		// 向伪终端写入Ctrl+C
		_, err := ptmx.Write([]byte{3})
		return err == nil
	}

	// 写入输入
	_, err := ptmx.Write([]byte(input))
	return err == nil
}

// 辅助函数 - 处理终端输出
func handleTerminalOutput(sessionID string, ptmx *os.File) {
	buffer := make([]byte, 1024)

	for {
		n, err := ptmx.Read(buffer)
		if n > 0 {
			SendResponse(&ResponseMessage{
				Command:   "terminal_output",
				Output:    string(buffer[:n]),
				SessionID: sessionID,
				Timestamp: time.Now().UnixMilli(),
			})
		}

		if err != nil {
			break
		}
	}
}

// 辅助函数 - 监控Shell进程
func monitorShellProcess(sessionID string, cmd *exec.Cmd) {
	err := cmd.Wait()

	exitStatus := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitStatus = status.ExitStatus()
			}
		}
	}

	// 发送退出消息
	SendResponse(&ResponseMessage{
		Command:   "terminal_output",
		Output:    fmt.Sprintf("\r\n[进程已退出，状态码: %d]\r\n", exitStatus),
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
	})

	// 清理资源
	closeTerminalSession(sessionID)
}

// 辅助函数 - 调整终端大小
func resizeTerminal(sessionID string, rows, cols uint16) bool {
	sessionPTYLock.RLock()
	ptmx, exists := sessionPTY[sessionID]
	sessionPTYLock.RUnlock()

	if !exists || ptmx == nil {
		return false
	}

	err := pty.Setsize(ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})

	return err == nil
}

// 辅助函数 - 关闭终端会话
func closeTerminalSession(sessionID string) bool {
	// 取消上下文
	sessionCancelLock.Lock()
	cancel, hasCancel := sessionCancels[sessionID]
	delete(sessionCancels, sessionID)
	sessionCancelLock.Unlock()

	if hasCancel {
		cancel()
	}

	// 关闭PTY
	sessionPTYLock.Lock()
	ptmx, hasPTY := sessionPTY[sessionID]
	delete(sessionPTY, sessionID)
	sessionPTYLock.Unlock()

	if hasPTY && ptmx != nil {
		ptmx.Close()
	}

	// 终止进程
	sessionProcessLock.Lock()
	pid, hasPid := sessionProcesses[sessionID]
	delete(sessionProcesses, sessionID)
	sessionProcessLock.Unlock()

	if hasPid {
		pgid, err := syscall.Getpgid(pid)
		if err == nil && pgid > 0 && runtime.GOOS != "darwin" {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	return true
}

// 辅助函数 - 执行命令
func executeCommand(cmdStr string, sessionID string, requestID string, ctx context.Context) (string, error) {
	// 解析命令
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", fmt.Errorf("空命令")
	}

	// 创建命令
	var cmd *exec.Cmd
	if len(parts) > 1 {
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, parts[0])
	}

	// 设置工作目录
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "/"
	}
	cmd.Dir = pwd

	// 设置环境变量
	cmd.Env = os.Environ()

	// 设置进程组
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// 创建输出管道
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return "", err
	}

	// 启动命令
	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return "", err
	}

	// 处理输出
	var outputBuffer strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	// 处理标准输出
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			outputBuffer.WriteString(line)
		}
	}()

	// 处理标准错误
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			outputBuffer.WriteString(line)
		}
	}()

	// 等待命令完成
	err = cmd.Wait()
	wg.Wait()

	return outputBuffer.String(), err
}

// 辅助函数 - 查找默认Shell
func findDefaultShell() string {
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}

	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	return "/bin/sh"
}

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
		SessionID: cmd.SessionID,
	}
}

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
		SessionID: cmd.SessionID,
	}
}

// InterruptCommand 中断正在执行的命令
func InterruptCommand(requestID string) bool {
	activeCommandsLock.RLock()
	cmd, exists := activeCommands[requestID]
	activeCommandsLock.RUnlock()

	if !exists || cmd == nil {
		return false
	}

	// 取消上下文
	cmd.Cancel()

	// 如果命令已经启动
	if cmd.Cmd.Process != nil {
		// 获取进程组ID
		pgid, err := syscall.Getpgid(cmd.Cmd.Process.Pid)
		if err != nil {
			log.Printf("[MQTT] 获取进程组ID失败: %v", err)
			pgid = 0
		}

		// 发送中断信号
		if pgid > 0 {
			syscall.Kill(-pgid, syscall.SIGINT)
			time.Sleep(100 * time.Millisecond)

			// 如果进程还在运行，发送SIGTERM
			if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
				syscall.Kill(-pgid, syscall.SIGTERM)
				time.Sleep(100 * time.Millisecond)

				// 如果进程还在运行，发送SIGKILL
				if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}
		} else {
			cmd.Cmd.Process.Kill()
		}
	}

	log.Printf("[MQTT] 已中断命令: %s", requestID)
	return true
}

// InterruptSessionCommand 通过会话ID中断命令
func InterruptSessionCommand(sessionID string) bool {
	if sessionID == "" {
		return false
	}

	activeCommandsLock.RLock()
	requestID, exists := sessionCommands[sessionID]
	activeCommandsLock.RUnlock()

	if !exists || requestID == "" {
		log.Printf("[MQTT] 未找到会话 %s 对应的活动命令", sessionID)
		return false
	}

	log.Printf("[MQTT] 通过会话ID %s 中断命令: %s", sessionID, requestID)
	return InterruptCommand(requestID)
}

// 注册终端相关命令处理器
func init() {
	// 确保已注册的处理器不会重复注册
	if _, exists := commandHandlers["interactiveShell"]; !exists {
		RegisterHandler("interactiveShell", &InteractiveShellHandler{})
	}
	if _, exists := commandHandlers["terminal_input"]; !exists {
		RegisterHandler("terminal_input", &TerminalInputHandler{})
	}
	if _, exists := commandHandlers["terminal_resize"]; !exists {
		RegisterHandler("terminal_resize", &TerminalResizeHandler{})
	}
	if _, exists := commandHandlers["closeTerminal"]; !exists {
		RegisterHandler("closeTerminal", &CloseTerminalHandler{})
	}
	if _, exists := commandHandlers["terminal_signal"]; !exists {
		RegisterHandler("terminal_signal", &TerminalSignalHandler{})
	}
}

// TerminalSignalHandler 处理终端信号
type TerminalSignalHandler struct{}

func (h *TerminalSignalHandler) Handle(cmd *CommandMessage) *ResponseMessage {
	if cmd.SessionID == "" || cmd.Signal == "" || TerminalMgr == nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   "缺少会话ID、信号类型或终端管理器未初始化",
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 获取终端
	term, err := TerminalMgr.GetTerminal(cmd.SessionID)
	if err != nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   fmt.Sprintf("找不到会话: %v", err),
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// 发送信号
	err = term.SendSignal(cmd.Signal)
	if err != nil {
		return &ResponseMessage{
			Command:   cmd.Command,
			RequestID: cmd.RequestID,
			Success:   false,
			Message:   fmt.Sprintf("发送信号失败: %v", err),
			SessionID: cmd.SessionID,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	return &ResponseMessage{
		Command:   cmd.Command,
		RequestID: cmd.RequestID,
		Success:   true,
		Message:   fmt.Sprintf("已发送信号: %s", cmd.Signal),
		SessionID: cmd.SessionID,
		Timestamp: time.Now().UnixMilli(),
	}
}

// HandleTerminalInput 处理终端输入的全局函数
func HandleTerminalInput(sessionID string, input string) bool {
	if sessionID == "" || input == "" || TerminalMgr == nil {
		return false
	}

	term, err := TerminalMgr.GetTerminal(sessionID)
	if err != nil {
		log.Printf("[MQTT] 找不到会话 %s: %v", sessionID, err)
		return false
	}

	// 发送输入到终端
	err = term.SendInput(input)
	if err != nil {
		log.Printf("[MQTT] 发送输入到会话 %s 失败: %v", sessionID, err)
		return false
	}

	return true
}
