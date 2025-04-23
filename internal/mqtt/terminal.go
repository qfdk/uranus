// internal/mqtt/terminal.go
package mqtt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ActiveCommand 表示正在执行的命令
type ActiveCommand struct {
	Cmd        *exec.Cmd          // 命令对象
	Cancel     context.CancelFunc // 取消上下文
	SessionID  string             // 会话ID
	RequestID  string             // 请求ID
	StartTime  time.Time          // 开始时间
	OutputChan chan string        // 输出通道
	Done       chan struct{}      // 完成信号
}

// MaxOutputLength 命令输出最大长度限制
const MaxOutputLength = 8192

// 命令会话管理
var (
	activeCommands     = make(map[string]*ActiveCommand) // 按请求ID索引的活动命令
	activeCommandsLock sync.RWMutex                      // 用于保护活动命令映射的锁
)

// executeTerminalCommand 执行终端命令并流式返回结果
func executeTerminalCommand(cmdStr string, sessionID string, requestID string, streaming bool) (string, error) {
	log.Printf("[MQTT] 执行终端命令: %s (会话: %s, 请求: %s, 流式: %v)", cmdStr, sessionID, requestID, streaming)

	// 去除命令前后的空白字符
	cmdStr = strings.TrimSpace(cmdStr)

	// 如果命令为空，返回错误
	if cmdStr == "" {
		return "", nil
	}

	// 检查命令是否安全
	if !IsCommandSafe(cmdStr) {
		return "错误: 检测到潜在危险命令，已拒绝执行", fmt.Errorf("dangerous command detected")
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	// 解析命令和参数
	parts := strings.Fields(cmdStr)
	var cmd *exec.Cmd
	if len(parts) > 1 {
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, parts[0])
	}

	// 设置进程组ID，以便后续可以终止整个进程组
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 创建输出管道
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("创建标准输出管道失败: %v", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("创建标准错误管道失败: %v", err)
	}

	// 如果是流式输出，设置输出通道
	outputChan := make(chan string, 100)
	done := make(chan struct{})

	// 保存活动命令，以便可以中断
	if streaming {
		activeCommandsLock.Lock()
		activeCommands[requestID] = &ActiveCommand{
			Cmd:        cmd,
			Cancel:     cancel,
			SessionID:  sessionID,
			RequestID:  requestID,
			StartTime:  time.Now(),
			OutputChan: outputChan,
			Done:       done,
		}
		activeCommandsLock.Unlock()

		// 设置清理函数
		defer func() {
			activeCommandsLock.Lock()
			delete(activeCommands, requestID)
			activeCommandsLock.Unlock()
		}()
	}

	// 启动命令
	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("启动命令失败: %v", err)
	}

	// 缓冲区用于收集输出
	var outputBuffer strings.Builder

	// 处理标准输出和错误的goroutine
	var wg sync.WaitGroup
	wg.Add(2)

	// 如果使用流式模式，则启动goroutine来发送输出
	if streaming {
		go func() {
			for output := range outputChan {
				// 发送输出到MQTT
				SendStreamingResponse(sessionID, requestID, output, false)
			}
			// 发送终止信号
			SendStreamingResponse(sessionID, requestID, "", true)
			close(done)
		}()
	}

	// 处理标准输出
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stdoutPipe)
		buffer := make([]byte, 4096)

		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				output := string(buffer[:n])

				// 保存到缓冲区
				outputBuffer.WriteString(output)

				// 如果启用了流式输出，将输出发送到通道
				if streaming {
					select {
					case outputChan <- output:
					default:
						// 通道已满，丢弃输出
						log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
					}
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[MQTT] 读取标准输出错误: %v", err)
				}
				break
			}
		}
	}()

	// 处理标准错误
	go func() {
		defer wg.Done()
		reader := bufio.NewReader(stderrPipe)
		buffer := make([]byte, 4096)

		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				output := string(buffer[:n])

				// 保存到缓冲区，带上错误标记
				outputBuffer.WriteString(output)

				// 如果启用了流式输出，将输出发送到通道
				if streaming {
					select {
					case outputChan <- output:
					default:
						// 通道已满，丢弃输出
						log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
					}
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[MQTT] 读取标准错误输出错误: %v", err)
				}
				break
			}
		}
	}()

	// 等待命令完成
	err = cmd.Wait()

	// 等待所有输出处理完成
	wg.Wait()

	// 关闭输出通道
	if streaming {
		close(outputChan)
		// 等待发送完成
		<-done
	}

	// 清理上下文
	cancel()

	// 获取完整输出
	output := outputBuffer.String()

	// 截断超长输出
	if len(output) > MaxOutputLength {
		output = output[:MaxOutputLength] + "\n... (输出被截断，超过最大长度限制)"
	}

	// 如果命令执行失败，但有输出，则仍返回输出
	if err != nil {
		if len(output) > 0 {
			return output, err
		}
		return fmt.Sprintf("命令执行失败: %v", err), err
	}

	return output, nil
}

// IsCommandSafe 检查命令是否安全
func IsCommandSafe(cmd string) bool {
	// 这里可以实现一些安全检查，例如禁止某些危险命令
	dangerousCmds := []string{
		"rm -rf /",
		"rm -fr /",
		":(){ :|:& };:", // fork炸弹
		"dd if=/dev/zero of=/dev/sda",
		"mkfs",
		"> /dev/sda",
		"mv /* /dev/null",
		"wget", // 暂时禁止下载命令
		"curl", // 暂时禁止网络请求命令
	}

	cmdLower := strings.ToLower(cmd)
	for _, dangerous := range dangerousCmds {
		if strings.Contains(cmdLower, dangerous) {
			log.Printf("[MQTT] 检测到危险命令: %s", cmd)
			return false
		}
	}

	return true
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

	// 如果命令已经启动，发送中断信号
	if cmd.Cmd.Process != nil {
		// 在Linux上终止整个进程组
		pgid, err := syscall.Getpgid(cmd.Cmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGTERM) // 发送SIGTERM到进程组

			// 给进程一些时间来清理
			time.Sleep(100 * time.Millisecond)

			// 如果进程还在运行，发送SIGKILL
			if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
				syscall.Kill(-pgid, syscall.SIGKILL)
			}
		} else {
			// 如果获取进程组ID失败，则直接终止进程
			cmd.Cmd.Process.Kill()
		}
	}

	log.Printf("[MQTT] 已中断命令: %s", requestID)
	return true
}

// SendStreamingResponse 发送流式响应
func SendStreamingResponse(sessionID string, requestID string, output string, final bool) {
	response := ResponseMessage{
		Command:   "execute",
		RequestID: requestID,
		Success:   true,
		Output:    output,
		SessionID: sessionID,
		Streaming: true,
		Final:     final,
		Timestamp: time.Now().UnixMilli(),
	}

	// 发送响应
	SendResponse(&response)
}
