// internal/terminal/terminal.go
// 完全基于MQTT的终端实现

package terminal

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Terminal 表示一个交互式终端会话
type Terminal struct {
	// 会话标识
	SessionID string

	// 命令进程
	Cmd *exec.Cmd

	// 伪终端
	Pty *os.File

	// MQTT 客户端
	MqttClient mqtt.Client

	// 主题名称
	InputTopic  string
	OutputTopic string

	// 控制通道
	Done chan struct{}

	// 上下文控制
	ctx    context.Context
	cancel context.CancelFunc

	// 窗口大小
	Rows uint16
	Cols uint16

	// 防止多次关闭
	closeLock sync.Mutex
	closed    bool
}

// NewTerminal 创建一个新的终端会话
func NewTerminal(sessionID string, mqttClient mqtt.Client, shell string) (*Terminal, error) {
	if shell == "" {
		shell = findDefaultShell()
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 创建命令
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = os.Environ()

	// 设置进程组ID，便于后续终止整个进程组
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// 创建伪终端
	ptmx, err := pty.Start(cmd)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建伪终端失败: %v", err)
	}

	// 设置默认终端大小
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    0,
		Y:    0,
	})

	// 创建终端对象
	terminal := &Terminal{
		SessionID:   sessionID,
		Cmd:         cmd,
		Pty:         ptmx,
		MqttClient:  mqttClient,
		InputTopic:  fmt.Sprintf("uranus/terminal/%s/input", sessionID),
		OutputTopic: fmt.Sprintf("uranus/terminal/%s/output", sessionID),
		Done:        make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		Rows:        24,
		Cols:        80,
	}

	// 订阅输入主题
	token := mqttClient.Subscribe(terminal.InputTopic, 1, terminal.handleInput)
	if token.Wait() && token.Error() != nil {
		cancel()
		ptmx.Close()
		return nil, fmt.Errorf("MQTT订阅失败: %v", token.Error())
	}

	// 启动输出处理循环
	go terminal.processOutput()

	// 启动进程监控
	go terminal.monitorProcess()

	log.Printf("[终端] 会话 %s 已启动，PID: %d", sessionID, cmd.Process.Pid)
	return terminal, nil
}

// Resize 调整终端窗口大小
func (t *Terminal) Resize(rows, cols uint16) error {
	t.Rows = rows
	t.Cols = cols
	return pty.Setsize(t.Pty, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})
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

	// 先尝试正常终止进程
	if t.Cmd.Process != nil {
		// 获取进程组ID
		pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
		if err == nil {
			// 向整个进程组发送SIGTERM信号
			syscall.Kill(-pgid, syscall.SIGTERM)

			// 等待进程终止
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-t.Done:
				timer.Stop()
			case <-timer.C:
				// 如果进程没有及时终止，发送SIGKILL
				syscall.Kill(-pgid, syscall.SIGKILL)
			}
		} else {
			// 如果无法获取进程组ID，直接终止进程
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

// handleInput 处理从MQTT接收到的输入
func (t *Terminal) handleInput(client mqtt.Client, msg mqtt.Message) {
	input := msg.Payload()

	// 检查是否为特殊控制序列
	if len(input) == 1 && input[0] == 3 { // Ctrl+C (ASCII 3)
		// 获取进程组ID并发送SIGINT
		if t.Cmd.Process != nil {
			pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGINT)
			} else {
				t.Cmd.Process.Signal(syscall.SIGINT)
			}
		}
	}

	// 写入伪终端
	if _, err := t.Pty.Write(input); err != nil {
		log.Printf("[终端] 会话 %s 写入失败: %v", t.SessionID, err)
	}
}

// processOutput 处理并发送终端输出
func (t *Terminal) processOutput() {
	buffer := make([]byte, 4096)

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
				output := buffer[:n]
				t.publishOutput(string(output))
			}
		}
	}
}

// publishOutput 发布输出到MQTT
func (t *Terminal) publishOutput(output string) {
	t.MqttClient.Publish(t.OutputTopic, 1, false, output)
}

// monitorProcess 监控命令进程状态
func (t *Terminal) monitorProcess() {
	// 等待命令结束
	err := t.Cmd.Wait()

	// 记录退出状态
	exitStatus := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitStatus = status.ExitStatus()
			}
		}
		log.Printf("[终端] 会话 %s 进程异常退出: %v, 状态码: %d", t.SessionID, err, exitStatus)
	} else {
		log.Printf("[终端] 会话 %s 进程正常退出", t.SessionID)
	}

	// 发送退出消息
	t.publishOutput(fmt.Sprintf("\r\n[进程已退出，状态码: %d]\r\n", exitStatus))

	// 关闭终端
	t.Close()
}

// findDefaultShell 查找系统中可用的默认shell
func findDefaultShell() string {
	// 按优先级尝试常见shell
	shells := []string{"/bin/zsh", "/bin/bash", "/bin/sh"}

	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	// 默认返回/bin/sh
	return "/bin/sh"
}
