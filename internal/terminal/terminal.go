// internal/terminal/terminal.go
package terminal

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
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

// 全局终端映射，用于直接访问PTY
var (
	ptyHandles   = make(map[string]*os.File)
	ptyHandleMux sync.RWMutex
)

// NewTerminal 创建一个新的终端会话
func NewTerminal(sessionID string, mqttClient mqtt.Client, shell string) (*Terminal, error) {
	if shell == "" {
		shell = findDefaultShell()
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 创建命令
	cmd := exec.CommandContext(ctx, shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	var ptmx *os.File
	var err error

	// 针对不同操作系统采用不同的策略
	if runtime.GOOS == "darwin" {
		// macOS 特殊处理
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true, // 只设置 Setsid，不设置 Setpgid
		}

		// 方式一：创建PTY
		ptmx, err = pty.Start(cmd)
		if err != nil {
			log.Printf("[终端] macOS PTY创建失败: %v, 尝试备选方案", err)

			// 备选方案：直接使用管道
			cmd = exec.CommandContext(ctx, shell)
			cmd.Env = append(os.Environ(), "TERM=xterm-256color")

			// 创建管道
			stdin, err := cmd.StdinPipe()
			if err != nil {
				cancel()
				return nil, fmt.Errorf("创建标准输入管道失败: %v", err)
			}

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				cancel()
				stdin.Close()
				return nil, fmt.Errorf("创建标准输出管道失败: %v", err)
			}

			stderr, err := cmd.StderrPipe()
			if err != nil {
				cancel()
				stdin.Close()
				stdout.Close()
				return nil, fmt.Errorf("创建标准错误管道失败: %v", err)
			}

			// 启动命令
			if err := cmd.Start(); err != nil {
				cancel()
				stdin.Close()
				stdout.Close()
				stderr.Close()
				return nil, fmt.Errorf("启动命令失败: %v", err)
			}

			// 创建自定义的PTY-like文件
			ptmx = &os.File{}

			// 启动goroutines来处理输出
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := stdout.Read(buf)
					if n > 0 {
						mqttClient.Publish(fmt.Sprintf("uranus/terminal/%s/output", sessionID), 1, false, buf[:n])
					}
					if err != nil {
						break
					}
				}
			}()

			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := stderr.Read(buf)
					if n > 0 {
						mqttClient.Publish(fmt.Sprintf("uranus/terminal/%s/output", sessionID), 1, false, buf[:n])
					}
					if err != nil {
						break
					}
				}
			}()

			// 存储stdin用于后续输入
			ptyHandleMux.Lock()
			ptyHandles[sessionID] = os.NewFile(stdin.(*os.File).Fd(), "stdin")
			ptyHandleMux.Unlock()
		} else {
			// 如果PTY创建成功，存储PTY句柄
			ptyHandleMux.Lock()
			ptyHandles[sessionID] = ptmx
			ptyHandleMux.Unlock()
		}
	} else {
		// Linux和其他系统 - 修改: 调整SysProcAttr配置
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Setsid:  true, // 添加Setsid，确保有效的控制终端
		}

		// 创建PTY
		ptmx, err = pty.Start(cmd)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建伪终端失败: %v", err)
		}

		// 存储PTY句柄
		ptyHandleMux.Lock()
		ptyHandles[sessionID] = ptmx
		ptyHandleMux.Unlock()
	}

	// 设置默认终端大小
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    0,
		Y:    0,
	})

	// 修改: 记录PTY句柄存储日志
	log.Printf("[终端] 会话 %s 的PTY句柄已存储", sessionID)

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

	// 修改: 调整日志记录
	log.Printf("[终端] 尝试调整会话 %s 大小为 %dx%d", t.SessionID, cols, rows)

	// 尝试直接访问终端句柄
	ptyHandleMux.RLock()
	ptyHandle, exists := ptyHandles[t.SessionID]
	ptyHandleMux.RUnlock()

	if exists && ptyHandle != nil {
		// 尝试调整大小
		err := pty.Setsize(ptyHandle, &pty.Winsize{
			Rows: rows,
			Cols: cols,
			X:    0,
			Y:    0,
		})

		if err != nil {
			log.Printf("[终端] 调整会话 %s 大小失败: %v", t.SessionID, err)
		} else {
			log.Printf("[终端] 会话 %s 大小已调整为 %dx%d", t.SessionID, cols, rows)
		}

		return err
	}

	// 修改: 如果通过映射找不到句柄，检查t.Pty是否可用
	if t.Pty == nil {
		log.Printf("[终端] 会话 %s 的PTY句柄不可用，无法调整大小", t.SessionID)
		return fmt.Errorf("PTY句柄不可用，无法调整大小")
	}

	// 如果没有句柄，使用t.Pty
	err := pty.Setsize(t.Pty, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})

	if err != nil {
		log.Printf("[终端] 使用实例PTY调整会话 %s 大小失败: %v", t.SessionID, err)
	} else {
		log.Printf("[终端] 使用实例PTY调整会话 %s 大小为 %dx%d", t.SessionID, cols, rows)
	}

	return err
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

	// 移除PTY句柄
	ptyHandleMux.Lock()
	delete(ptyHandles, t.SessionID)
	ptyHandleMux.Unlock()

	// 先尝试正常终止进程
	if t.Cmd.Process != nil {
		// 获取进程组ID
		pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
		if err == nil && runtime.GOOS != "darwin" {
			// 向整个进程组发送SIGTERM信号 (在非macOS系统上)
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
			// 在macOS上或无法获取进程组ID时直接终止进程
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
		// 根据系统不同采用不同的中断策略
		if runtime.GOOS == "darwin" {
			// macOS上不使用进程组ID
			if t.Cmd.Process != nil {
				t.Cmd.Process.Signal(syscall.SIGINT)
			}
		} else {
			// 其他系统尝试使用进程组ID
			if t.Cmd.Process != nil {
				pgid, err := syscall.Getpgid(t.Cmd.Process.Pid)
				if err == nil {
					syscall.Kill(-pgid, syscall.SIGINT)
				} else {
					t.Cmd.Process.Signal(syscall.SIGINT)
				}
			}
		}
	}

	// 尝试直接访问PTY句柄
	ptyHandleMux.RLock()
	ptyHandle, exists := ptyHandles[t.SessionID]
	ptyHandleMux.RUnlock()

	if exists && ptyHandle != nil {
		// 写入输入
		if _, err := ptyHandle.Write(input); err != nil {
			log.Printf("[终端] 会话 %s 写入失败: %v", t.SessionID, err)
		}
	} else {
		// 回退到t.Pty
		if _, err := t.Pty.Write(input); err != nil {
			log.Printf("[终端] 会话 %s 写入失败: %v", t.SessionID, err)
		}
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

// ResizeBySessionID 通过会话ID调整终端大小（用于外部直接调用）
func ResizeBySessionID(sessionID string, rows, cols uint16) error {
	// 修改: 增加详细日志记录
	log.Printf("[终端] 尝试通过会话ID %s 调整终端大小为 %dx%d", sessionID, cols, rows)

	ptyHandleMux.RLock()
	ptyHandle, exists := ptyHandles[sessionID]
	ptyHandleMux.RUnlock()

	if !exists || ptyHandle == nil {
		log.Printf("[终端] 找不到会话 %s 的PTY句柄", sessionID)
		return fmt.Errorf("找不到会话 %s 的PTY句柄", sessionID)
	}

	// 调整终端大小
	err := pty.Setsize(ptyHandle, &pty.Winsize{
		Rows: rows,
		Cols: cols,
		X:    0,
		Y:    0,
	})

	if err != nil {
		log.Printf("[终端] 通过会话ID调整大小失败: %v", err)
		return err
	}

	log.Printf("[终端] 通过会话ID成功调整会话 %s 大小为 %dx%d", sessionID, cols, rows)
	return nil
}
