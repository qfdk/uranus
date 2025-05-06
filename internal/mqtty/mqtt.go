package mqtty

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"uranus/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var mqttClient mqtt.Client

const (
	TopicInput = "input"

	TopicControl = "control"
	TopicResize  = "resize"
)

// 消息结构
type Message struct {
	SessionID string      `json:"sessionId"`
	Type      string      `json:"type,omitempty"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
	RequestId string      `json:"requestId,omitempty"`
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

			// 发送离线状态消息（遗嘱消息）
			// 注意：由于连接已断开，我们无法发送，但MQTT服务器会自动发送遗嘱消息
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

// 处理从命令主题接收到的消息
func handleCommandMessage(client mqtt.Client, msg mqtt.Message, topicPrefix string, manager *SessionManager, agentUuid string) {
	// 解析消息内容
	var command struct {
		Command   string      `json:"command"`
		RequestId string      `json:"requestId"`
		ClientId  string      `json:"clientId"`
		Type      string      `json:"type"`
		SessionId string      `json:"sessionId"`
		Data      interface{} `json:"data"`
	}

	if err := json.Unmarshal(msg.Payload(), &command); err != nil {
		log.Printf("[MQTTY] 解析命令消息失败: %v", err)
		return
	}

	log.Printf("[MQTTY] 收到命令: %s", msg.Payload())

	// 终端命令另行处理
	if command.Command == "terminal" {
		handleTerminalCommand(client, command, manager, agentUuid, topicPrefix)
		return
	}

	// 其他命令类型的处理继续原来的逻辑
	// 转换为正确的消息格式
	message := Message{
		SessionID: command.SessionId,
		Type:      command.Type,
		Data:      command.Data,
		Timestamp: time.Now().UnixNano() / 1e6,
		RequestId: command.RequestId,
	}

	// 根据命令类型处理
	switch command.Type {
	case "create":
		handleControlMessage(command.SessionId, &message, manager, topicPrefix)

		// 发送带请求ID的成功响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   true,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "created",
			Message:   "终端会话已创建",
		}

		// 发送响应
		responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)

	case "input":
		handleInputMessage(command.SessionId, &message, manager)

	case "resize":
		handleResizeMessage(command.SessionId, &message, manager)

	case "close":
		handleControlMessage(command.SessionId, &message, manager, topicPrefix)

		// 发送带请求ID的成功响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   true,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "closed",
			Message:   "终端会话已关闭",
		}

		// 发送响应
		responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)
	}
}

// 处理接收到的消息
func handleMessage(client mqtt.Client, msg mqtt.Message, topicPrefix string, manager *SessionManager) {
	topic := msg.Topic()
	_, msgType, ok := parseTopicParts(topic, topicPrefix)
	if !ok {
		log.Printf("[MQTTY] 无法解析主题: %s", topic)
		return
	}

	var message Message
	if err := json.Unmarshal(msg.Payload(), &message); err != nil {
		log.Printf("[MQTTY] 解析消息失败: %v", err)
		return
	}

	// 从消息中获取sessionID
	sessionID := message.SessionID

	switch msgType {
	case TopicInput:
		handleInputMessage(sessionID, &message, manager)
	case TopicControl:
		handleControlMessage(sessionID, &message, manager, topicPrefix)
	case TopicResize:
		handleResizeMessage(sessionID, &message, manager)
	}
}

// 处理输入消息
func handleInputMessage(sessionID string, msg *Message, manager *SessionManager) {
	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 会话不存在: %s", sessionID)
		return
	}

	// 转换数据
	var input string
	switch v := msg.Data.(type) {
	case string:
		input = v
	case map[string]interface{}:
		// 处理可能是map形式的data字段
		if data, ok := v["data"].(string); ok {
			input = data
		} else {
			log.Printf("[MQTTY] 找不到有效的输入数据: %+v", v)
			return
		}
	default:
		log.Printf("[MQTTY] 无效的输入格式: %T", msg.Data)
		return
	}

	// 发送到会话
	if err := session.SendInput([]byte(input)); err != nil {
		log.Printf("[MQTTY] 发送输入失败: %v", err)
	}
}

// 处理控制消息
func handleControlMessage(sessionID string, msg *Message, manager *SessionManager, topicPrefix string) {
	// Type字段是字符串，直接使用
	msgType := msg.Type

	switch msgType {
	case "create":
		// 获取要使用的shell
		var shell string
		if shellData, ok := msg.Data.(string); ok && shellData != "" {
			shell = shellData
		} else {
			shell = getDefaultShell()
		}

		// 检查会话是否已经存在并且活跃
		session, err := manager.GetSession(sessionID)
		if err == nil && session != nil {
			// 会话存在，检查是否已关闭
			var isClosed bool
			select {
			case <-session.Done:
				isClosed = true
			default:
				isClosed = false
			}

			if !isClosed {
				// 会话存在且活跃，直接复用
				log.Printf("[MQTTY] 复用已存在的活跃会话: %s", sessionID)

				// 发送创建状态
				publishStatus("created")

				// 确保输出转发正在运行
				go forwardSessionOutput(topicPrefix, sessionID, manager)
				return
			}
		}

		// 创建会话（如果不存在或已关闭）
		err = manager.CreateSession(sessionID, shell)
		if err != nil {
			log.Printf("[MQTTY] 创建会话失败: %v", err)
			// 发送错误状态
			log.Printf("[MQTTY] 发送错误状态")
			publishStatus("error")
		} else {
			// 发送创建成功状态
			publishStatus("created")
			// 启动输出转发
			go forwardSessionOutput(topicPrefix, sessionID, manager)
		}

	case "close":
		// 关闭会话
		err := manager.CloseSession(sessionID)
		if err != nil {
			log.Printf("[MQTTY] 关闭会话失败: %v", err)
			// 发送错误状态
			publishStatus("error")
		} else {
			// 发送关闭状态
			publishStatus("closed")
		}
	}
}

// 处理调整大小消息
func handleResizeMessage(sessionID string, msg *Message, manager *SessionManager) {
	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 会话不存在: %s", sessionID)
		return
	}

	// 解析尺寸数据
	var resizeData map[string]interface{}
	var ok bool

	// 处理不同的数据结构
	switch v := msg.Data.(type) {
	case map[string]interface{}:
		// 检查是否包含了嵌套的data字段
		if nestedData, hasNestedData := v["data"].(map[string]interface{}); hasNestedData {
			resizeData = nestedData
		} else {
			// 使用直接的结构
			resizeData = v
		}
		ok = true
	default:
		log.Printf("[MQTTY] 无效的调整大小数据: %T", msg.Data)
		return
	}

	if !ok || resizeData == nil {
		log.Printf("[MQTTY] 无法解析终端大小数据: %v", msg.Data)
		return
	}

	// 获取行列数据
	var rows, cols float64
	var rowsOK, colsOK bool

	rows, rowsOK = resizeData["rows"].(float64)
	cols, colsOK = resizeData["cols"].(float64)

	if !rowsOK || !colsOK {
		log.Printf("[MQTTY] 缺少行列数据: rows=%v, cols=%v", resizeData["rows"], resizeData["cols"])
		return
	}

	log.Printf("[MQTTY] 调整终端大小: 会话=%s, 行=%d, 列=%d", sessionID, int(rows), int(cols))
	if err := session.Resize(uint16(rows), uint16(cols)); err != nil {
		log.Printf("[MQTTY] 调整终端大小失败: %v", err)
	}
}

// 全局变量用于跟踪活跃的转发会话
var (
	forwardingSessions = make(map[string]chan struct{})
	forwardingMutex    sync.Mutex
)

// 转发会话输出 - 兼容旧版本
// 使用配置文件中的 UUID
func forwardSessionOutput(topicPrefix, sessionID string, manager *SessionManager) {
	// 获取Agent UUID用于前端响应主题
	agentUuid := config.GetAppConfig().UUID
	// 调用新版本的函数
	forwardSessionOutputWithUUID(topicPrefix, sessionID, manager, agentUuid)
}

// 解析主题部分
func parseTopicParts(topic, prefix string) (sessionID, msgType string, ok bool) {
	if !strings.HasPrefix(topic, prefix+"/") {
		return "", "", false
	}
	action := strings.TrimPrefix(topic, prefix+"/")
	return "", action, true
}
