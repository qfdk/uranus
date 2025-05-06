package mqtty

import (
	"context"
	"log"
	"os"
	"uranus/internal/config"
)

// Options MQTT终端配置选项
type Options struct {
	BrokerURL    string
	ClientID     string
	Username     string
	Password     string
	DefaultShell string
	BufferSize   int
	TopicPrefix  string
}

// Terminal MQTT终端服务器
type Terminal struct {
	options Options
	manager *SessionManager
	ctx     context.Context
	cancel  context.CancelFunc
}

// DefaultOptions 返回默认配置
func DefaultOptions() Options {
	appConfig := config.GetAppConfig()
	mqttBroker := appConfig.MQTTBroker
	if mqttBroker == "" {
		mqttBroker = "mqtt://mqtt.qfdk.me:1883" // 默认MQTT服务器地址
	}
	return Options{
		BrokerURL:    mqttBroker,
		ClientID:     "mqtty-server-" + appConfig.UUID,
		DefaultShell: "/bin/bash",
		BufferSize:   4096,
		TopicPrefix:  "uranus/terminal",
	}
}

// NewTerminal 创建新的MQTT终端服务
func NewTerminal(opts Options) *Terminal {
	ctx, cancel := context.WithCancel(context.Background())
	return &Terminal{
		options: opts,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 启动MQTT终端服务
func (t *Terminal) Start() error {
	log.Println("[MQTTY] 正在启动MQTT终端服务...")

	// 初始化会话管理器
	t.manager = NewSessionManager()

	// 初始化MQTT处理
	if err := InitMQTT(t.options, t.manager); err != nil {
		return err
	}

	// 注册终端消息处理器
	RegisterTerminalHandlers(t.options, t.manager)

	// 启动心跳服务
	log.Printf("[进程][%d]: 启动MQTT心跳服务", os.Getpid())
	go StartHeartbeat(t.ctx)

	log.Println("[MQTTY] MQTT终端服务已启动")
	return nil
}

// Stop 停止MQTT终端服务
func (t *Terminal) Stop() {
	t.cancel()
	if t.manager != nil {
		t.manager.CloseAll()
	}
	DisconnectMQTT()
	log.Println("[MQTTY] MQTT终端服务已停止")
}
