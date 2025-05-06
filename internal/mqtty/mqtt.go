package mqtty

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"uranus/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTT主题定义
const (
	// 心跳和状态主题
	HeartbeatTopic = "uranus/heartbeat"
	StatusTopic    = "uranus/status"

	// 终端相关主题
	TopicInput   = "input"
	TopicControl = "control"
	TopicResize  = "resize"
)

var (
	mqttClient mqtt.Client
	// 全局变量用于跟踪活跃的转发会话
	forwardingSessions = make(map[string]chan struct{})
	forwardingMutex    sync.RWMutex
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

// 消息结构
type Message struct {
	SessionID string      `json:"sessionId"`
	Type      string      `json:"type,omitempty"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
	RequestId string      `json:"requestId,omitempty"`
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

// InitMQTT 初始化MQTT连接
func InitMQTT(opts Options, manager *SessionManager) error {
	if mqttClient != nil && mqttClient.IsConnected() {
		log.Println("[MQTTY] MQTT客户端已连接")
		return nil
	}

	mqttOpts := mqtt.NewClientOptions().
		AddBroker(opts.BrokerURL).
		SetClientID(opts.ClientID).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetKeepAlive(30 * time.Second).
		SetConnectionLostHandler(func(client mqtt.Client, err error) {
			log.Printf("[MQTTY] MQTT连接丢失: %v", err)
			log.Printf("[MQTTY] 连接丢失，自动发送离线状态")
		}).
		SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
			log.Printf("[MQTTY] MQTT正在重连...")
		}).
		SetOnConnectHandler(func(client mqtt.Client) {
			log.Printf("[MQTTY] MQTT连接成功")
			// 订阅主题
			subscribeTopics(client, opts.TopicPrefix, manager)
		})

	// 创建遗嘱消息，在连接异常断开时自动发送
	appConfig := config.GetAppConfig()
	willMsg := map[string]interface{}{
		"uuid":      appConfig.UUID,
		"status":    "offline",
		"timestamp": time.Now(),
	}

	// 序列化遗嘱消息
	willMsgBytes, _ := json.Marshal(willMsg)

	// 设置遗嘱消息
	mqttOpts.SetWill(StatusTopic, string(willMsgBytes), 1, true)

	if opts.Username != "" {
		mqttOpts.SetUsername(opts.Username)
		if opts.Password != "" {
			mqttOpts.SetPassword(opts.Password)
		}
	}

	mqttClient = mqtt.NewClient(mqttOpts)
	token := mqttClient.Connect()

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT连接失败: %v", token.Error())
	}

	return nil
}

// DisconnectMQTT 断开MQTT连接
func DisconnectMQTT() {
	if mqttClient != nil && mqttClient.IsConnected() {
		// 在断开连接前先发送离线状态
		log.Println("[MQTTY] 发送离线状态后断开连接")
		publishStatus("offline")

		// 然后断开连接
		mqttClient.Disconnect(250)
		log.Println("[MQTTY] MQTT已断开连接")
	}
}

// 订阅所需主题
func subscribeTopics(client mqtt.Client, topicPrefix string, manager *SessionManager) {
	topics := []string{
		fmt.Sprintf("%s/%s", topicPrefix, TopicInput),
		fmt.Sprintf("%s/%s", topicPrefix, TopicControl),
		fmt.Sprintf("%s/%s", topicPrefix, TopicResize),
	}

	for _, topic := range topics {
		token := client.Subscribe(topic, 1, func(client mqtt.Client, msg mqtt.Message) {
			handleMessage(client, msg, topicPrefix, manager)
		})

		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTTY] 订阅主题失败 %s: %v", topic, token.Error())
		} else {
			log.Printf("[MQTTY] 已订阅主题: %s", topic)
		}
	}

	// 订阅通过代理转发的命令主题
	// 需要注意的是，前端可能会使用 uranus/command/{UUID} 的主题格式
	// 从配置中获取UUID
	agentUuid := config.GetAppConfig().UUID
	commandTopic := fmt.Sprintf("uranus/command/%s", agentUuid)
	token := client.Subscribe(commandTopic, 1, func(client mqtt.Client, msg mqtt.Message) {
		handleCommandMessage(client, msg, topicPrefix, manager, agentUuid)
	})

	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 订阅命令主题失败 %s: %v", commandTopic, token.Error())
	} else {
		log.Printf("[MQTTY] 已订阅命令主题: %s", commandTopic)
	}
}

// 解析主题部分
func parseTopicParts(topic, prefix string) (sessionID, msgType string, ok bool) {
	if !strings.HasPrefix(topic, prefix+"/") {
		return "", "", false
	}
	action := strings.TrimPrefix(topic, prefix+"/")
	return "", action, true
}
