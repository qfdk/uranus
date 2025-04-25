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
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

var (
	activeTerminals = make(map[string]*os.File) // 会话ID -> PTY映射
	terminalMutex   sync.RWMutex                // 保护上述映射的互斥锁
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
	Output        string             // 累积的输出
	CommandName   string             // 命令名称
	IsSpecial     bool               // 是否为特殊命令(如ping)
}

// MaxOutputLength 命令输出最大长度限制
const MaxOutputLength = 8192

// 命令会话管理
var (
	activeCommands     = make(map[string]*ActiveCommand) // 按请求ID索引的活动命令
	activeCommandsLock sync.RWMutex                      // 用于保护活动命令映射的锁
	sessionCommands    = make(map[string]string)         // 会话ID -> 请求ID映射
)

// 特殊命令列表 - 需要特殊处理中断的命令
var specialCommands = []string{
	"ping", "traceroute", "top", "htop", "telnet", "ssh",
	"nc", "netcat", "tail", "tcpdump", "watch", "nslookup",
}

// 判断命令是否为交互式
func isInteractiveCommand(cmdStr string) bool {
	interactiveCmds := []string{
		"vim", "vi", "nano", "emacs", "pico", "less", "more",
		"top", "htop", "mysql", "psql", "mongo", "redis-cli",
		"ssh", "telnet", "tmux", "screen",
	}

	cmdFields := strings.Fields(cmdStr)
	if len(cmdFields) == 0 {
		return false
	}

	cmdName := cmdFields[0]
	for _, icmd := range interactiveCmds {
		if cmdName == icmd {
			return true
		}
	}
	return false
}

// 判断命令是否为特殊命令
func isSpecialCommand(cmdStr string) bool {
	cmdFields := strings.Fields(cmdStr)
	if len(cmdFields) == 0 {
		return false
	}

	cmdName := cmdFields[0]
	for _, scmd := range specialCommands {
		if cmdName == scmd {
			return true
		}
	}
	return false
}

// 获取命令名称
func getCommandName(cmdStr string) string {
	cmdFields := strings.Fields(cmdStr)
	if len(cmdFields) == 0 {
		return ""
	}
	return cmdFields[0]
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

	// 判断是否为特殊命令（如ping）
	isSpecial := isSpecialCommand(cmdStr)
	commandName := getCommandName(cmdStr)

	// 解析命令和参数
	parts := strings.Fields(cmdStr)
	var cmd *exec.Cmd
	if len(parts) > 1 {
		cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	} else {
		cmd = exec.CommandContext(ctx, parts[0])
	}

	// 设置命令工作目录为当前目录
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "/"
	}
	cmd.Dir = pwd

	// 继承当前进程的环境变量
	cmd.Env = os.Environ()

	// 设置进程组ID，以便后续可以终止整个进程组
	// 修改: 为解决PTY问题，调整SysProcAttr设置
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // 创建新的进程组
		Setsid:  true, // 创建新的会话
	}

	// 增加调试日志
	log.Printf("[MQTT] 命令准备执行: %s，工作目录: %s", cmdStr, cmd.Dir)

	// 如果是流式输出，设置输出通道
	outputChan := make(chan string, 100)
	done := make(chan struct{})
	var inputChan chan string

	// 处理交互式命令和普通命令的区别
	var cmdOutput string
	var cmdErr error

	if interactive || isInteractiveCommand(cmdStr) {
		interactive = true
		log.Printf("[MQTT] 启动交互式命令: %s", cmdStr)

		// 为交互式命令创建输入通道
		inputChan = make(chan string, 100)

		// 使用伪终端(pty)来处理交互式命令
		cmdOutput, cmdErr = executeInteractiveCommand(cmd, sessionID, requestID, ctx, cancel, outputChan, done, inputChan)
	} else {
		// 使用标准管道处理非交互式命令
		cmdOutput, cmdErr = executeNonInteractiveCommand(cmd, sessionID, requestID, ctx, cancel, outputChan, done)
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
			Output:        cmdOutput,
			CommandName:   commandName,
			IsSpecial:     isSpecial,
		}

		if interactive && inputChan != nil {
			activeCommand.InputChan = inputChan
		}

		activeCommands[requestID] = activeCommand

		// 更新会话到命令的映射
		if sessionID != "" {
			sessionCommands[sessionID] = requestID
			log.Printf("[MQTT] 添加会话映射: %s -> %s", sessionID, requestID)
		}

		activeCommandsLock.Unlock()

		// 设置清理函数
		defer func() {
			activeCommandsLock.Lock()
			delete(activeCommands, requestID)
			// 如果会话ID存在且映射到此请求ID，也清理会话映射
			if sessionID != "" && sessionCommands[sessionID] == requestID {
				delete(sessionCommands, sessionID)
				log.Printf("[MQTT] 删除会话映射: %s", sessionID)
			}
			activeCommandsLock.Unlock()
		}()
	} else {
		// 非流式模式直接清理上下文
		defer cancel()
	}

	// 如果没有输出但有错误，将错误信息作为输出返回
	if cmdOutput == "" && cmdErr != nil {
		return fmt.Sprintf("命令执行错误: %v", cmdErr), cmdErr
	}

	return cmdOutput, cmdErr
}

// executeInteractiveCommand 使用伪终端执行交互式命令
func executeInteractiveCommand(cmd *exec.Cmd, sessionID string, requestID string,
	ctx context.Context, cancel context.CancelFunc,
	outputChan chan string, done chan struct{}, inputChan chan string) (string, error) {

	// 修改: 调整SysProcAttr设置，以解决伪终端问题
	// 根据不同操作系统设置不同的SysProcAttr
	if runtime.GOOS == "darwin" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true, // 在macOS上只设置Setsid
		}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Setsid:  true, // 添加Setsid，确保有效的控制终端
		}
	}

	// 创建伪终端
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[MQTT] 伪终端启动失败 (%v)，回退到标准管道模式", err)
		// 发送警告信息给客户端
		outputChan <- "警告: 无法启动完全交互模式，某些交互式命令可能无法正常工作\n"

		// 重要：确保在出错时cancel已设置的上下文
		cancel()

		// 创建新的上下文和取消函数用于非交互模式
		newCtx, newCancel := context.WithTimeout(context.Background(), 30*time.Minute)

		// 重新创建命令，确保没有之前的配置
		newCmd := exec.CommandContext(newCtx, cmd.Path)
		if len(cmd.Args) > 1 {
			newCmd.Args = cmd.Args
		}
		newCmd.Env = os.Environ()
		newCmd.Dir = cmd.Dir

		// 针对macOS设置特殊的进程属性
		if runtime.GOOS == "darwin" {
			newCmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
				Setsid:  true, // 确保设置Setsid而不是Setctty
			}
		} else {
			newCmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
				Setsid:  true, // 添加Setsid
			}
		}

		// 回退到非交互式命令处理
		return executeNonInteractiveCommand(newCmd, sessionID, requestID, newCtx, newCancel, outputChan, done)
	}

	defer ptmx.Close()

	// 将PTY存储到全局映射 - 修改: 确保正确存储PTY句柄
	terminalMutex.Lock()
	activeTerminals[sessionID] = ptmx
	terminalMutex.Unlock()

	// 修改: 同时在ptyHandles中存储PTY句柄，确保终端大小调整可以工作
	ptyHandleMux.Lock()
	ptyHandles[sessionID] = ptmx
	ptyHandleMux.Unlock()

	log.Printf("[MQTT] 交互式命令已启动，PID: %d，已存储PTY句柄到会话ID: %s", cmd.Process.Pid, sessionID)

	// 保存伪终端引用
	activeCommandsLock.Lock()
	if activeCmd, exists := activeCommands[requestID]; exists && activeCmd != nil {
		activeCmd.Pty = ptmx
		// 获取并记录进程组ID，便于后续精确中断
		if activeCmd.Cmd.Process != nil {
			pgid, err := syscall.Getpgid(activeCmd.Cmd.Process.Pid)
			if err == nil {
				log.Printf("[MQTT] 交互式命令的进程组ID: %d", pgid)
			}
		}
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
						log.Printf("[MQTT] 交互式输出: %d 字节", n)
					default:
						log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
					}
				}

				if err != nil {
					if err != io.EOF {
						log.Printf("[MQTT] 读取伪终端输出错误: %v", err)
					} else {
						log.Printf("[MQTT] 伪终端输出已结束 (EOF)")
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

					// 获取进程组ID
					if cmd.Process != nil {
						// 先向进程组发送SIGINT信号
						pgid, err := syscall.Getpgid(cmd.Process.Pid)
						if err == nil && pgid > 0 {
							// 向整个进程组发送SIGINT信号
							log.Printf("[MQTT] 发送SIGINT到进程组: %d", pgid)
							syscall.Kill(-pgid, syscall.SIGINT)
						} else {
							// 如果无法获取进程组ID，发送给主进程
							log.Printf("[MQTT] 发送SIGINT到进程: %d", cmd.Process.Pid)
							syscall.Kill(cmd.Process.Pid, syscall.SIGINT)
						}

						// 同时将Ctrl+C写入伪终端，某些程序会拦截此信号
						_, err = ptmx.Write([]byte{3})
						if err != nil {
							log.Printf("[MQTT] 写入Ctrl+C到伪终端失败: %v", err)
						}

						// 检查命令名称并应用特殊处理
						activeCommandsLock.RLock()
						activeCmd := activeCommands[requestID]
						isSpecial := false
						cmdName := ""
						if activeCmd != nil {
							isSpecial = activeCmd.IsSpecial
							cmdName = activeCmd.CommandName
						}
						activeCommandsLock.RUnlock()

						// 对于特殊命令（如ping）进行额外处理
						if isSpecial {
							log.Printf("[MQTT] 对特殊命令 %s 应用强制中断策略", cmdName)

							// 尝试不同类型的信号
							signals := []syscall.Signal{
								syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL,
							}

							// 立即发送多个信号
							for _, sig := range signals {
								if pgid > 0 {
									log.Printf("[MQTT] 发送信号 %v 到进程组: %d", sig, pgid)
									syscall.Kill(-pgid, sig)
								} else {
									log.Printf("[MQTT] 发送信号 %v 到进程: %d", sig, cmd.Process.Pid)
									syscall.Kill(cmd.Process.Pid, sig)
								}
								time.Sleep(50 * time.Millisecond)
							}

							// 尝试使用命令终止特定进程
							go func() {
								if cmdName == "ping" {
									terminateCmd := exec.Command("pkill", "-9", "ping")
									terminateCmd.Run()

									// 再尝试 killall
									terminateCmd = exec.Command("killall", "-9", "ping")
									terminateCmd.Run()
								} else if len(cmdName) > 0 {
									terminateCmd := exec.Command("pkill", "-9", cmdName)
									terminateCmd.Run()

									terminateCmd = exec.Command("killall", "-9", cmdName)
									terminateCmd.Run()
								}
							}()
						}

						// 延迟一点时间后检查进程是否仍在运行
						go func() {
							time.Sleep(500 * time.Millisecond)

							// 检查进程是否仍在运行
							if cmd.Process != nil && cmd.ProcessState == nil {
								log.Printf("[MQTT] 进程未退出，发送SIGTERM信号")

								// 再次发送SIGTERM信号
								pgid, err := syscall.Getpgid(cmd.Process.Pid)
								if err == nil && pgid > 0 {
									log.Printf("[MQTT] 发送SIGTERM到进程组: %d", pgid)
									syscall.Kill(-pgid, syscall.SIGTERM)
								} else {
									syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
								}

								// 最后检查是否需要SIGKILL
								time.Sleep(500 * time.Millisecond)
								if cmd.Process != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
									log.Printf("[MQTT] 进程仍未退出，发送SIGKILL信号")
									if pgid > 0 {
										log.Printf("[MQTT] 发送SIGKILL到进程组: %d", pgid)
										syscall.Kill(-pgid, syscall.SIGKILL)
									} else {
										syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
									}
								}
							}
						}()
					}
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
	if cmdErr != nil {
		log.Printf("[MQTT] 交互式命令执行完成但返回错误: %v", cmdErr)
	} else {
		log.Printf("[MQTT] 交互式命令执行成功完成")
	}

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
		log.Printf("[MQTT] 创建标准输出管道失败: %v", err)
		cancel()

		// 确保通道被正确关闭
		select {
		case _, ok := <-outputChan:
			if ok {
				close(outputChan)
			}
		default:
			close(outputChan)
		}

		// 确保done通道被关闭
		select {
		case _, ok := <-done:
			if ok {
				close(done)
			}
		default:
			close(done)
		}

		return "", fmt.Errorf("创建标准输出管道失败: %v", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[MQTT] 创建标准错误管道失败: %v", err)
		cancel()

		// 关闭已创建的管道
		stdoutPipe.Close()

		// 确保通道被正确关闭
		select {
		case _, ok := <-outputChan:
			if ok {
				close(outputChan)
			}
		default:
			close(outputChan)
		}

		// 确保done通道被关闭
		select {
		case _, ok := <-done:
			if ok {
				close(done)
			}
		default:
			close(done)
		}

		return "", fmt.Errorf("创建标准错误管道失败: %v", err)
	}

	// 增加启动前日志
	log.Printf("[MQTT] 启动非交互式命令: %s %v", cmd.Path, cmd.Args)

	// 启动命令
	if err := cmd.Start(); err != nil {
		log.Printf("[MQTT] 启动命令失败: %v", err)
		cancel()

		// 关闭已创建的管道
		stdoutPipe.Close()
		stderrPipe.Close()

		// 确保通道被正确关闭
		select {
		case _, ok := <-outputChan:
			if ok {
				close(outputChan)
			}
		default:
			close(outputChan)
		}

		// 确保done通道被关闭
		select {
		case _, ok := <-done:
			if ok {
				close(done)
			}
		default:
			close(done)
		}

		return "", fmt.Errorf("启动命令失败: %v", err)
	}

	log.Printf("[MQTT] 命令已启动，PID: %d", cmd.Process.Pid)

	// 检查是否为特殊命令
	activeCommandsLock.RLock()
	activeCmd := activeCommands[requestID]
	isSpecial := false
	if activeCmd != nil {
		isSpecial = activeCmd.IsSpecial
	}
	activeCommandsLock.RUnlock()

	// 对于特殊命令如ping，设置进程和进程组ID，确保可以正确终止
	if isSpecial && cmd.Process != nil {
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err == nil {
			log.Printf("[MQTT] 特殊命令的进程组ID: %d", pgid)
		}
	}

	// 缓冲区用于收集输出
	var outputBuffer strings.Builder

	// 处理标准输出和错误的goroutine
	var wg sync.WaitGroup
	wg.Add(2)

	// 启动goroutine发送输出 - 修改这部分，保证累积所有输出
	go func() {
		var allOutput strings.Builder

		for output := range outputChan {
			// 累积所有输出，确保最终响应包含完整内容
			allOutput.WriteString(output)

			// 发送流式更新 - 不是最终响应
			SendStreamingResponse(sessionID, requestID, output, false)
		}

		// 最终响应，包含整个输出内容
		finalOutput := allOutput.String()
		log.Printf("[MQTT] 发送最终响应，总长度: %d 字节", len(finalOutput))
		SendStreamingResponse(sessionID, requestID, finalOutput, true)
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
					preview := output
					if len(preview) > 30 {
						preview = preview[:30] + "..."
					}
					log.Printf("[MQTT] 标准输出: %d 字节, 内容预览: %s", n, strings.ReplaceAll(preview, "\n", "\\n"))
				default:
					// 通道已满，丢弃输出
					log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[MQTT] 读取标准输出错误: %v", err)
				} else {
					log.Printf("[MQTT] 标准输出已结束 (EOF)")
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
					preview := output
					if len(preview) > 30 {
						preview = preview[:30] + "..."
					}
					log.Printf("[MQTT] 标准错误: %d 字节, 内容预览: %s", n, strings.ReplaceAll(preview, "\n", "\\n"))
				default:
					// 通道已满，丢弃输出
					log.Printf("[MQTT] 输出通道已满，丢弃部分输出")
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[MQTT] 读取标准错误输出错误: %v", err)
				} else {
					log.Printf("[MQTT] 标准错误已结束 (EOF)")
				}
				break
			}
		}
	}()

	// 等待命令完成
	err = cmd.Wait()
	if err != nil {
		log.Printf("[MQTT] 命令执行完成但返回错误: %v", err)
	} else {
		log.Printf("[MQTT] 命令执行成功完成")
	}

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

// 添加PTY句柄全局访问 - 用于外部模块访问PTY句柄
var (
	ptyHandles   = make(map[string]*os.File) // 会话ID -> PTY句柄映射
	ptyHandleMux sync.RWMutex                // 保护映射的互斥锁
)

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

// SendStreamingResponse 发送流式响应
func SendStreamingResponse(sessionID string, requestID string, output string, final bool) {
	// 添加调试日志
	if final {
		log.Printf("[MQTT] 发送最终流式响应: requestID=%s, 输出长度=%d字节", requestID, len(output))
	} else {
		log.Printf("[MQTT] 发送流式更新: requestID=%s, 片段长度=%d字节", requestID, len(output))
	}

	// 创建响应对象
	response := ResponseMessage{
		Command:   "execute",
		RequestID: requestID,
		Success:   true,
		Output:    output, // 确保输出包含在响应中
		SessionID: sessionID,
		Streaming: true,
		Final:     final,
		Timestamp: time.Now().UnixMilli(),
	}

	// 添加适当的消息
	if final {
		response.Message = "命令执行完成"
	}

	// 发送响应
	SendResponse(&response)
}
