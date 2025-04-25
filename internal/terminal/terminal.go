// internal/terminal/terminal.go
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"uranus/internal/config"

	"github.com/creack/pty"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Terminal 表示交互式终端会话
type Terminal struct {
	// 基本信息
	SessionID  string
	Cmd        *exec.Cmd
	Pty        *os.File
	MqttClient mqtt.Client

	// 通信主题
	InputTopic  string
	OutputTopic string

	// 控制通道
	Done chan struct{}

	// 上下文控制
	cancel context.CancelFunc
	ctx    context.Context

	// 终端大小
	Rows uint16
	Cols uint16

	// 防止多次关闭
	closeLock sync.Mutex
	closed    bool

	// 输出缓冲区
	outputBuffer []byte
	bufferMutex  sync.Mutex
}

// 全局终端映射
var (
	terminals   = make(map[string]*Terminal)
	terminalMux sync.RWMutex
)

// NewTerminal 创建新的终端会话
func NewTerminal(sessionID string, mqttClient mqtt.Client, shell string) (*Terminal, error) {
	// 使用默认shell
	if shell == "" {
		shell = findDefaultShell()
	}

	// 创建可取消上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 创建命令
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// 简化进程属性
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	// 创建PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建伪终端失败: %v", err)
	}

	// 设置初始终端大小
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    0,
		Y:    0,
	})

	// 创建终端对象
	terminal := &Terminal{
		SessionID:    sessionID,
		Cmd:          cmd,
		Pty:          ptmx,
		MqttClient:   mqttClient,
		InputTopic:   fmt.Sprintf("uranus/terminal/%s/input", sessionID),
		OutputTopic:  fmt.Sprintf("uranus/terminal/%s/output", sessionID),
		Done:         make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
		Rows:         24,
		Cols:         80,
		outputBuffer: make([]byte, 0, 4096),
	}

	// 订阅输入主题
	token := mqttClient.Subscribe(terminal.InputTopic, 1, terminal.handleInput)
	if token.Wait() && token.Error() != nil {
		cancel()
		ptmx.Close()
		return nil, fmt.Errorf("MQTT订阅失败: %v", token.Error())
	}

	// 保存到全局映射
	terminalMux.Lock()
	terminals[sessionID] = terminal
	terminalMux.Unlock()

	// 启动输出处理和进程监控
	go terminal.processOutput()
	go terminal.monitorProcess()

	log.Printf("[终端] 会话 %s 已启动，PID: %d", sessionID, cmd.Process.Pid)
	return terminal, nil
}

// 配置进程属性，适配不同操作系统
func configureProcessAttrs(cmd *exec.Cmd) {
	if runtime.GOOS == "darwin" {
		// macOS特殊处理
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
	} else {
		// Linux和其他系统
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Setsid:  true,
		}
	}
}

// Resize 调整终端窗口大小
func (t *Terminal) Resize(rows, cols uint16) error {
	t.Rows = rows
	t.Cols = cols

	// 调整终端大小
	err := pty.Setsize(t.Pty, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})

	if err != nil {
		log.Printf("[终端] 调整会话 %s 大小失败: %v", t.SessionID, err)
		return err
	}

	log.Printf("[终端] 会话 %s 大小已调整为 %dx%d", t.SessionID, cols, rows)
	return nil
}

// Close 关闭终端会话
func (t *Terminal) Close() error {
	t.closeLock.Lock()
	defer t.closeLock.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	// 取消MQTT订阅
	t.MqttClient.Unsubscribe(t.InputTopic)

	// 发送终止消息
	t.publishOutput("\r\n[终端会话已关闭]\r\n")

	// 从全局映射中移除
	terminalMux.Lock()
	delete(terminals, t.SessionID)
	terminalMux.Unlock()

	// 终止进程
	if t.Cmd.Process != nil {
		pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)

		// 尝试优雅关闭
		if err == nil && runtime.GOOS != "darwin" && pgid > 0 {
			syscall.Kill(-pgid, syscall.SIGTERM)

			// 等待进程退出
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-t.Done:
				timer.Stop()
			case <-timer.C:
				// 如果进程没有及时终止，发送SIGKILL
				syscall.Kill(-pgid, syscall.SIGKILL)
			}
		} else {
			// 直接终止进程
			t.Cmd.Process.Kill()
		}
	}

	// 取消上下文
	t.cancel()

	// 关闭伪终端
	if t.Pty != nil {
		t.Pty.Close()
	}

	log.Printf("[终端] 会话 %s 已关闭", t.SessionID)
	return nil
}

// 处理输入
func (t *Terminal) handleInput(client mqtt.Client, msg mqtt.Message) {
	input := msg.Payload()

	// 检查是否为特殊控制序列(Ctrl+C)
	if len(input) == 1 && input[0] == 3 {
		log.Printf("[终端] 收到会话 %s 的中断信号", t.SessionID)

		// 根据系统不同采用不同的中断策略
		if t.Cmd.Process != nil {
			if runtime.GOOS == "darwin" {
				// macOS上直接向进程发送信号
				t.Cmd.Process.Signal(syscall.SIGINT)
			} else {
				// 其他系统尝试使用进程组ID
				pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
				if err == nil && pgid > 0 {
					syscall.Kill(-pgid, syscall.SIGINT)
				} else {
					t.Cmd.Process.Signal(syscall.SIGINT)
				}
			}
		}
	}

	// 写入输入到终端
	if _, err := t.Pty.Write(input); err != nil {
		log.Printf("[终端] 会话 %s 写入失败: %v", t.SessionID, err)
	}
}

// 处理输出
func (t *Terminal) processOutput() {
	buffer := make([]byte, 4096)
	lastSendTime := time.Now()

	// 批处理间隔
	const batchInterval = 30 * time.Millisecond

	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			n, err := t.Pty.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Printf("[终端] 会话 %s 读取错误: %v", t.SessionID, err)
				}
				close(t.Done)
				return
			}

			if n > 0 {
				// 将输出存入缓冲区
				t.bufferMutex.Lock()
				t.outputBuffer = append(t.outputBuffer, buffer[:n]...)
				shouldSend := len(t.outputBuffer) > 2048 || time.Since(lastSendTime) > batchInterval

				if shouldSend {
					// 发送批量输出
					output := make([]byte, len(t.outputBuffer))
					copy(output, t.outputBuffer)
					t.outputBuffer = t.outputBuffer[:0]
					lastSendTime = time.Now()
					t.bufferMutex.Unlock()

					t.publishOutput(string(output))
				} else {
					t.bufferMutex.Unlock()
				}
			}
		}
	}
}

// 发布输出到MQTT
func (t *Terminal) publishOutput(output string) {
	if token := t.MqttClient.Publish(t.OutputTopic, 1, false, output); token.Wait() && token.Error() != nil {
		log.Printf("[终端] 会话 %s 发布输出失败: %v", t.SessionID, token.Error())
	}

	// 同时发送标准响应格式
	t.publishResponseMessage(output)
}

// 发布响应消息(与标准命令响应格式兼容)
func (t *Terminal) publishResponseMessage(output string) {
	type ResponseMessage struct {
		Command   string      `json:"command"`
		RequestID string      `json:"requestId,omitempty"`
		Success   bool        `json:"success"`
		Message   string      `json:"message,omitempty"`
		Output    string      `json:"output,omitempty"`
		Data      interface{} `json:"data,omitempty"`
		Timestamp int64       `json:"timestamp"`
		SessionID string      `json:"sessionId,omitempty"`
		Streaming bool        `json:"streaming,omitempty"`
		Final     bool        `json:"final,omitempty"`
	}

	appConfig := config.GetAppConfig()
	responseTopic := "uranus/response/" + appConfig.UUID

	response := &ResponseMessage{
		Command:   "terminal_output",
		Output:    output,
		SessionID: t.SessionID,
		Timestamp: time.Now().UnixMilli(),
		Success:   true,
	}

	payload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[终端] 会话 %s 序列化响应失败: %v", t.SessionID, err)
		return
	}

	if token := t.MqttClient.Publish(responseTopic, 1, false, payload); token.Wait() && token.Error() != nil {
		log.Printf("[终端] 会话 %s 发布响应失败: %v", t.SessionID, token.Error())
	}
}

// 监控进程
func (t *Terminal) monitorProcess() {
	err := t.Cmd.Wait()

	exitStatus := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitStatus = status.ExitStatus()
			}
		}
	}

	// 发送退出消息
	t.publishOutput(fmt.Sprintf("\r\n[进程已退出，状态码: %d]\r\n", exitStatus))

	// 清理资源
	t.Close()
}

// SendInput 发送输入到终端
func (t *Terminal) SendInput(input string) error {
	// 特殊处理Ctrl+C (ASCII 3)
	if input == "\u0003" {
		if t.Cmd.Process != nil {
			pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)

			// 发送中断信号
			if err == nil && pgid > 0 && runtime.GOOS != "darwin" {
				// 向进程组发送信号
				syscall.Kill(-pgid, syscall.SIGINT)
			} else {
				// 向进程发送信号
				t.Cmd.Process.Signal(syscall.SIGINT)
			}
		}
	}

	// 写入输入
	_, err := t.Pty.Write([]byte(input))
	return err
}

// SendSignal 发送信号到终端进程
func (t *Terminal) SendSignal(signal string) error {
	if t.Cmd.Process == nil {
		return errors.New("进程不存在")
	}

	// 获取进程组ID
	pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
	if err != nil {
		return err
	}

	// 根据信号类型发送对应的信号
	var sig syscall.Signal
	switch signal {
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGQUIT":
		sig = syscall.SIGQUIT
	case "SIGHUP":
		sig = syscall.SIGHUP
	case "SIGTSTP":
		sig = syscall.SIGTSTP
	case "SIGCONT":
		sig = syscall.SIGCONT
	default:
		return errors.New("不支持的信号类型")
	}

	// 向进程组发送信号
	if pgid > 0 && runtime.GOOS != "darwin" {
		return syscall.Kill(-pgid, sig)
	}

	// 向单个进程发送信号
	return t.Cmd.Process.Signal(sig)
}

// 查找默认shell
func findDefaultShell() string {
	shells := []string{"/bin/zsh", "/bin/bash", "/bin/sh"}

	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	return "/bin/sh"
}
