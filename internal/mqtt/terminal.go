// internal/mqtt/terminal.go
package mqtt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// ActiveCommand 表示正在执行的命令
type ActiveCommand struct {
	Cmd           *exec.Cmd          // 命令对象
	Cancel        context.CancelFunc // 取消上下文
	SessionID     string             // 会话ID
	RequestID     string             // 请求ID
	StartTime     time.Time          // 开始时间
	OutputChan    chan string        // 输出通道
	Done          chan struct{}      // 完成信号
	Pty           *os.File           // 伪终端文件描述符
	IsInteractive bool               // 是否为交互式命令
	InputChan     chan string        // 交互式输入通道
}

// MaxOutputLength 命令输出最大长度限制
const MaxOutputLength = 8192

// 命令会话管理
var (
	activeCommands     = make(map[string]*ActiveCommand) // 按请求ID索引的活动命令
	activeCommandsLock sync.RWMutex                      // 用于保护活动命令映射的锁
	sessionCommands    = make(map[string]string)         // 会话ID -> 请求ID映射
)

// 判断命令是否为交互式
func isInteractiveCommand(cmdStr string) bool {
	interactiveCmds := []string{
		"vim", "vi", "nano", "emacs", "pico", "less", "more",
		"top", "htop", "mysql", "psql", "mongo", "redis-cli",
	}

	cmdName := strings.Fields(cmdStr)[0]
	for _, icmd := range interactiveCmds {
		if cmdName == icmd {
			return true
		}
	}
	return false
}

// 获取会话关联的所有进程ID
func getProcessesBySession(sessionID string) []int {
	if sessionID == "" {
		return nil
	}

	var pids []int

	activeCommandsLock.RLock()
	defer activeCommandsLock.RUnlock()

	// 查找关联的命令
	for _, cmd := range activeCommands {
		if cmd.SessionID == sessionID && cmd.Cmd != nil && cmd.Cmd.Process != nil {
			pids = append(pids, cmd.Cmd.Process.Pid)

			// 尝试获取进程组
			pgid, err := syscall.Getpgid(cmd.Cmd.Process.Pid)
			if err == nil && pgid > 0 {
				pids = append(pids, -pgid) // 负值表示进程组
			}
		}
	}

	return pids
}

// executeTerminalCommand 执行终端命令并流式返回结果
func executeTerminalCommand(cmdStr string, sessionID string, requestID string, streaming bool, interactive bool) (string, error) {
	log.Printf("[MQTT] 执行终端命令: %s (会话: %s, 请求: %s, 流式: %v, 交互式: %v)",
		cmdStr, sessionID, requestID, streaming, interactive)

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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

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

	// 如果是流式输出，设置输出通道
	outputChan := make(chan string, 100)
	done := make(chan struct{})
	var inputChan chan string

	// 处理交互式命令和普通命令的区别
	var cmdOutput string
	var err error

	if interactive || isInteractiveCommand(cmdStr) {
		interactive = true
		log.Printf("[MQTT] 启动交互式命令: %s", cmdStr)

		// 为交互式命令创建输入通道
		inputChan = make(chan string, 100)

		// 使用伪终端(pty)来处理交互式命令
		cmdOutput, err = executeInteractiveCommand(cmd, sessionID, requestID, ctx, cancel, outputChan, done, inputChan)
	} else {
		// 使用标准管道处理非交互式命令
		cmdOutput, err = executeNonInteractiveCommand(cmd, sessionID, requestID, ctx, cancel, outputChan, done)
	}

	// 保存活动命令，以便可以中断
	if streaming {
		activeCommandsLock.Lock()
		activeCommand := &ActiveCommand{
			Cmd:           cmd,
			Cancel:        cancel,
			SessionID:     sessionID,
			RequestID:     requestID,
			StartTime:     time.Now(),
			OutputChan:    outputChan,
			Done:          done,
			IsInteractive: interactive,
		}

		if interactive && inputChan != nil {
			activeCommand.InputChan = inputChan
		}

		activeCommands[requestID] = activeCommand

		// 更新会话到命令的映射
		if sessionID != "" {
			sessionCommands[sessionID] = requestID
		}

		activeCommandsLock.Unlock()

		// 设置清理函数
		defer func() {
			activeCommandsLock.Lock()
			delete(activeCommands, requestID)
			// 如果会话ID存在且映射到此请求ID，也清理会话映射
			if sessionID != "" && sessionCommands[sessionID] == requestID {
				delete(sessionCommands, sessionID)
			}
			activeCommandsLock.Unlock()
		}()
	} else {
		// 非流式模式直接清理上下文
		defer cancel()
	}

	return cmdOutput, err
}

// executeInteractiveCommand 使用伪终端执行交互式命令
func executeInteractiveCommand(cmd *exec.Cmd, sessionID string, requestID string,
	ctx context.Context, cancel context.CancelFunc,
	outputChan chan string, done chan struct{}, inputChan chan string) (string, error) {

	// 创建伪终端
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[MQTT] 伪终端启动失败 (%v)，回退到标准管道模式", err)
		// 发送警告信息给客户端
		outputChan <- "警告: 无法启动完全交互模式，某些交互式命令可能无法正常工作\n"

		// 回退到非交互式命令处理
		return executeNonInteractiveCommand(cmd, sessionID, requestID, ctx, cancel, outputChan, done)
	}
	defer ptmx.Close()

	// 保存伪终端引用
	activeCommandsLock.Lock()
	if cmd, exists := activeCommands[requestID]; exists && cmd != nil {
		cmd.Pty = ptmx
	}
	activeCommandsLock.Unlock()

	// 设置终端大小为标准80x24
	pty.Setsize(ptmx, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    0,
		Y:    0,
	})

	// 处理输出
	var outputBuffer strings.Builder
	outputDone := make(chan struct{})

	// 启动一个goroutine读取pty输出
	go func() {
		defer close(outputDone)

		buffer := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return // 上下文取消时退出
			default:
				n, err := ptmx.Read(buffer)
				if n > 0 {
					output := string(buffer[:n])

					// 保存到缓冲区
					outputBuffer.WriteString(output)

					// 发送到输出通道
					select {
					case outputChan <- output:
					default:
						log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
					}
				}

				if err != nil {
					if err != io.EOF {
						log.Printf("[MQTT] 读取伪终端输出错误: %v", err)
					}
					return
				}
			}
		}
	}()

	// 处理输入传递到伪终端
	go func() {
		for {
			select {
			case input, ok := <-inputChan:
				if !ok {
					return // 通道已关闭
				}

				// 检查是否是Ctrl+C (ASCII值3)
				if input == "\u0003" {
					log.Printf("[MQTT] 收到Ctrl+C，发送中断信号到进程组")

					// 直接向进程发送SIGINT信号
					if cmd.Process != nil {
						syscall.Kill(cmd.Process.Pid, syscall.SIGINT)
					}

					// 同时将Ctrl+C写入伪终端，某些程序会拦截此信号
					_, err := ptmx.Write([]byte{3})
					if err != nil {
						log.Printf("[MQTT] 写入Ctrl+C到伪终端失败: %v", err)
					}

					// 延迟一点时间后，如果进程仍在运行，尝试强制终止
					go func() {
						time.Sleep(500 * time.Millisecond)
						if cmd.Process != nil && cmd.ProcessState == nil {
							// 进程仍在运行，尝试获取进程组并终止整个组
							pgid, err := syscall.Getpgid(cmd.Process.Pid)
							if err == nil {
								syscall.Kill(-pgid, syscall.SIGTERM)
								log.Printf("[MQTT] 发送SIGTERM到进程组: %d", pgid)

								// 再给一些时间
								time.Sleep(300 * time.Millisecond)

								// 如果仍在运行，强制终止
								if cmd.ProcessState == nil {
									syscall.Kill(-pgid, syscall.SIGKILL)
									log.Printf("[MQTT] 发送SIGKILL到进程组: %d", pgid)
								}
							}
						}
					}()
				} else {
					// 正常输入，写入伪终端
					_, err := ptmx.Write([]byte(input))
					if err != nil {
						log.Printf("[MQTT] 写入伪终端失败: %v", err)
					}
				}

			case <-ctx.Done():
				return // 上下文取消
			}
		}
	}()

	// 等待命令完成
	cmdErr := cmd.Wait()

	// 等待所有输出处理完成
	<-outputDone

	// 关闭输出通道
	close(outputChan)

	// 等待发送完成
	<-done

	// 获取完整输出
	output := outputBuffer.String()

	// 截断超长输出
	if len(output) > MaxOutputLength {
		output = output[:MaxOutputLength] + "\n... (输出被截断，超过最大长度限制)"
	}

	return output, cmdErr
}

// executeNonInteractiveCommand 使用标准管道执行非交互式命令
func executeNonInteractiveCommand(cmd *exec.Cmd, sessionID string, requestID string,
	ctx context.Context, cancel context.CancelFunc,
	outputChan chan string, done chan struct{}) (string, error) {

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

	// 启动goroutine发送输出
	go func() {
		for output := range outputChan {
			// 发送输出到MQTT
			SendStreamingResponse(sessionID, requestID, output, false)
		}
		// 发送终止信号
		SendStreamingResponse(sessionID, requestID, "", true)
		close(done)
	}()

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
				select {
				case outputChan <- output:
				default:
					// 通道已满，丢弃输出
					log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
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
				select {
				case outputChan <- output:
				default:
					// 通道已满，丢弃输出
					log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
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
	close(outputChan)

	// 等待发送完成
	<-done

	// 获取完整输出
	output := outputBuffer.String()

	// 截断超长输出
	if len(output) > MaxOutputLength {
		output = output[:MaxOutputLength] + "\n... (输出被截断，超过最大长度限制)"
	}

	return output, err
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

	// 如果命令已经启动
	if cmd.Cmd.Process != nil {
		// 如果是交互式命令且有伪终端
		if cmd.IsInteractive && cmd.Pty != nil {
			// 发送中断信号（Ctrl+C）到伪终端
			_, err := cmd.Pty.Write([]byte{3}) // ASCII码3是Ctrl+C
			if err != nil {
				log.Printf("[MQTT] 发送Ctrl+C到伪终端失败: %v", err)
			}

			// 给命令一些时间处理中断
			time.Sleep(300 * time.Millisecond)

			// 直接向进程发送SIGINT
			syscall.Kill(cmd.Cmd.Process.Pid, syscall.SIGINT)
			log.Printf("[MQTT] 发送SIGINT到进程: %d", cmd.Cmd.Process.Pid)
		}

		// 在Linux上终止整个进程组
		pgid, err := syscall.Getpgid(cmd.Cmd.Process.Pid)
		if err == nil {
			// 尝试优雅地终止进程组
			syscall.Kill(-pgid, syscall.SIGINT) // 先尝试SIGINT

			// 给进程一些时间响应
			time.Sleep(100 * time.Millisecond)

			// 如果仍在运行，发送SIGTERM
			if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
				syscall.Kill(-pgid, syscall.SIGTERM)

				// 再给一些时间
				time.Sleep(100 * time.Millisecond)

				// 如果进程还在运行，发送SIGKILL
				if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}
		} else {
			// 如果获取进程组ID失败，则直接终止进程
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
		return false
	}

	return InterruptCommand(requestID)
}

// HandleTerminalInput 处理终端输入
func HandleTerminalInput(sessionID string, input string) bool {
	if sessionID == "" || input == "" {
		return false
	}

	activeCommandsLock.RLock()
	requestID, exists := sessionCommands[sessionID]
	if !exists || requestID == "" {
		activeCommandsLock.RUnlock()
		return false
	}

	cmd, exists := activeCommands[requestID]
	activeCommandsLock.RUnlock()

	if !exists || cmd == nil || !cmd.IsInteractive || cmd.InputChan == nil {
		return false
	}

	// 特殊处理Ctrl+C (ASCII值3)
	if input == "\u0003" {
		log.Printf("[MQTT] 收到Ctrl+C请求，会话ID: %s", sessionID)

		// 多种方式尝试中断

		// 1. 发送Ctrl+C到伪终端
		if cmd.Pty != nil {
			_, err := cmd.Pty.Write([]byte{3})
			if err != nil {
				log.Printf("[MQTT] 发送Ctrl+C到伪终端失败: %v", err)
			}
		}

		// 2. 直接向进程发送SIGINT
		if cmd.Cmd.Process != nil {
			// 发送给进程
			syscall.Kill(cmd.Cmd.Process.Pid, syscall.SIGINT)
			log.Printf("[MQTT] 发送SIGINT到进程: %d", cmd.Cmd.Process.Pid)

			// 发送给进程组
			pgid, err := syscall.Getpgid(cmd.Cmd.Process.Pid)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGINT)
				log.Printf("[MQTT] 发送SIGINT到进程组: %d", pgid)
			}

			// 启动定时器，如果进程没有在短时间内退出，则发送更强的信号
			go func() {
				time.Sleep(300 * time.Millisecond)

				// 检查进程是否仍在运行
				if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
					// 发送更强的信号
					syscall.Kill(cmd.Cmd.Process.Pid, syscall.SIGTERM)
					log.Printf("[MQTT] 发送SIGTERM到进程: %d", cmd.Cmd.Process.Pid)

					if pgid > 0 {
						syscall.Kill(-pgid, syscall.SIGTERM)
						log.Printf("[MQTT] 发送SIGTERM到进程组: %d", pgid)
					}

					// 再次等待
					time.Sleep(300 * time.Millisecond)

					// 最终发送SIGKILL
					if cmd.Cmd.ProcessState == nil || !cmd.Cmd.ProcessState.Exited() {
						syscall.Kill(cmd.Cmd.Process.Pid, syscall.SIGKILL)
						log.Printf("[MQTT] 发送SIGKILL到进程: %d", cmd.Cmd.Process.Pid)

						if pgid > 0 {
							syscall.Kill(-pgid, syscall.SIGKILL)
							log.Printf("[MQTT] 发送SIGKILL到进程组: %d", pgid)
						}
					}
				}
			}()
		}

		// 仍将输入发送到通道
		select {
		case cmd.InputChan <- input:
			return true
		default:
			return false
		}
	}

	// 常规输入处理
	select {
	case cmd.InputChan <- input:
		return true
	default:
		log.Printf("[MQTT] 输入通道已满，丢弃输入")
		return false
	}
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
