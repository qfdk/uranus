// 替换 internal/mqtt/main.go 内容

package mqtt

import (
	"context"
	"log"
	"uranus/internal/terminal"
)

// Init 初始化MQTT模块并启动心跳服务
func Init(ctx context.Context) {
	log.Println("[MQTT] 模块初始化")

	// 初始化MQTT连接
	if err := InitMQTT(); err != nil {
		log.Printf("[MQTT] 初始化失败: %v", err)
		return
	}

	// 创建终端管理器
	TerminalMgr = terminal.NewManager(GetClient())

	log.Println("[MQTT] 终端管理器已初始化")

	// 启动MQTT心跳服务
	go StartHeartbeat(ctx)

	// 监听ctx取消信号，关闭所有终端
	go func() {
		<-ctx.Done()
		log.Println("[MQTT] 收到关闭信号，清理所有终端会话")
		if TerminalMgr != nil {
			TerminalMgr.CloseAll()
		}
	}()
}
