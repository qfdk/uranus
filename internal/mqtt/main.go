// internal/mqtt/main.go
package mqtt

import (
	"context"
	"log"
)

// Init 初始化MQTT模块并启动心跳服务
func Init(ctx context.Context) {
	log.Println("[MQTT] 模块初始化")

	// 启动MQTT心跳服务
	go StartHeartbeat(ctx)
}
