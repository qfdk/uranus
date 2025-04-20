// internal/tools/command.go
package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExecuteCommand 执行shell命令并返回输出
func ExecuteCommand(command string) (string, error) {
	// 日志命令执行
	fmt.Printf("[Command] 执行命令: %s\n", command)

	// 分割命令和参数
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("空命令")
	}

	cmd := parts[0]
	args := []string{}

	if len(parts) > 1 {
		args = parts[1:]
	}

	// 创建执行命令
	execCmd := exec.Command(cmd, args...)

	// 捕获标准输出和错误
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// 设置执行超时
	timeout := 30 * time.Second

	// 启动命令
	if err := execCmd.Start(); err != nil {
		return "", fmt.Errorf("启动命令失败: %v", err)
	}

	// 创建超时通道
	done := make(chan error, 1)
	go func() {
		done <- execCmd.Wait()
	}()

	// 等待命令完成或超时
	select {
	case <-time.After(timeout):
		// 命令超时，尝试终止
		if err := execCmd.Process.Kill(); err != nil {
			fmt.Printf("终止超时命令失败: %v\n", err)
		}
		return "", fmt.Errorf("命令执行超时(超过%v)", timeout)

	case err := <-done:
		// 命令完成
		output := stdout.String()
		errOutput := stderr.String()

		// 如果有错误输出且没有正常输出，返回错误输出
		if err != nil {
			if output == "" && errOutput != "" {
				output = errOutput
			}
			return output, err
		}

		// 如果有错误输出但命令返回成功，将错误输出附加到标准输出
		if errOutput != "" {
			if output != "" {
				output += "\n" + errOutput
			} else {
				output = errOutput
			}
		}

		return output, nil
	}
}
