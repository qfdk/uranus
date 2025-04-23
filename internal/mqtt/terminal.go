// internal/mqtt/terminal.go
package mqtt

import (
	"log"
	"strings"
	"uranus/internal/tools"
)

// MaxOutputLength 命令输出最大长度限制
const MaxOutputLength = 4096

// executeTerminalCommand 执行终端命令并返回结果
func executeTerminalCommand(cmdStr string) (string, error) {
	log.Printf("[MQTT] 执行终端命令: %s", cmdStr)

	// 去除命令前后的空白字符
	cmdStr = strings.TrimSpace(cmdStr)

	// 如果命令为空，返回错误
	if cmdStr == "" {
		return "", nil
	}

	// 使用tools包中的ExecuteCommand执行命令
	output, err := tools.ExecuteCommand(cmdStr)

	// 限制输出长度，避免过大的数据导致MQTT传输问题
	if len(output) > MaxOutputLength {
		output = output[:MaxOutputLength] + "\n... (输出被截断，超过最大长度限制)"
		log.Printf("[MQTT] 命令输出被截断，原长度: %d", len(output))
	}

	return output, err
}

// IsCommandSafe 检查命令是否安全（可以根据需要扩展限制）
func IsCommandSafe(cmd string) bool {
	// 这里可以实现一些安全检查，例如禁止某些危险命令
	// 例如: rm -rf /, 格式化磁盘等

	// 当前简单实现，可以根据需要扩展
	dangerousCmds := []string{
		"rm -rf /",
		"rm -fr /",
		"mkfs",
		"dd if=/dev/zero",
		":(){ :|:& };:", // fork炸弹
	}

	for _, dangerous := range dangerousCmds {
		if strings.Contains(cmd, dangerous) {
			log.Printf("[MQTT] 检测到危险命令: %s", cmd)
			return false
		}
	}

	return true
}

// IsAdminCommand 检查是否为管理员命令（需要特殊权限）
func IsAdminCommand(cmd string) bool {
	adminPrefixes := []string{
		"sudo ",
		"su ",
	}

	for _, prefix := range adminPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}

	return false
}

// FormatOutput 格式化命令输出，支持转义特殊字符
func FormatOutput(output string) string {
	// 当前简单实现，可以根据需要扩展
	// 例如添加ANSI颜色代码转换为HTML等

	// 转换为HTML安全的字符
	output = strings.ReplaceAll(output, "<", "&lt;")
	output = strings.ReplaceAll(output, ">", "&gt;")

	return output
}
