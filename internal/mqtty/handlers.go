package mqtty

import (
	"log"
)

// RegisterTerminalHandlers 注册终端处理器
func RegisterTerminalHandlers(opts Options, manager *SessionManager) {
	log.Println("[MQTTY] 注册终端处理器")

	// 这里可以添加更多处理器注册代码
	// 目前处理逻辑已经在mqtt.go中实现
}
