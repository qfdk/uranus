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

var (
	mqttClient    mqtt.Client
	mqttConnected bool
)

// MQTT主题常量
const (
	TopicInput   = "input"
	TopicOutput  = "output"
	TopicControl = "control"
	TopicResize  = "resize"
	TopicStatus  = "status"
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
			mqttConnected = false
		}).
		SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
			log.Printf("[MQTTY] MQTT正在重连...")
		}).
		SetOnConnectHandler(func(client mqtt.Client) {
			log.Printf("[MQTTY] MQTT连接成功")
			mqttConnected = true

			// 订阅主题
			subscribeTopics(client, opts.TopicPrefix, manager)
		})

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
		mqttClient.Disconnect(250)
		log.Println("[MQTTY] MQTT已断开连接")
	}
	mqttConnected = false
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
		log.Printf("[MQTT] 订阅命令主题失败 %s: %v", commandTopic, token.Error())
	} else {
		log.Printf("[MQTT] 已订阅命令主题: %s", commandTopic)
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
		log.Printf("[MQTT] 解析命令消息失败: %v", err)
		return
	}

	log.Printf("[MQTT] 收到终端命令: %s", msg.Payload())

	// 只处理终端相关命令
	if command.Command != "terminal" {
		return
	}

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
				publishStatus(topicPrefix, sessionID, "created", nil)
				// 确保输出转发正在运行
				go forwardSessionOutput(topicPrefix, sessionID, manager)
				return
			}
		}

		// 创建会话（如果不存在或已关闭）
		err = manager.CreateSession(sessionID, shell)
		if err != nil {
			log.Printf("[MQTTY] 创建会话失败: %v", err)
			publishStatus(topicPrefix, sessionID, "error", err.Error())
		} else {
			publishStatus(topicPrefix, sessionID, "created", nil)
			// 启动输出转发
			go forwardSessionOutput(topicPrefix, sessionID, manager)
		}

	case "close":
		// 关闭会话
		err := manager.CloseSession(sessionID)
		if err != nil {
			log.Printf("[MQTTY] 关闭会话失败: %v", err)
			publishStatus(topicPrefix, sessionID, "error", err.Error())
		} else {
			publishStatus(topicPrefix, sessionID, "closed", nil)
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

// 发布状态消息
func publishStatus(topicPrefix, sessionID, status string, data interface{}) {
	topic := fmt.Sprintf("%s/%s/%s", topicPrefix, sessionID, TopicStatus)

	message := Message{
		SessionID: sessionID,
		Type:      status,
		Data:      data,
		Timestamp: time.Now().UnixNano() / 1e6,
	}

	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("[MQTTY] 序列化状态消息失败: %v", err)
		return
	}

	if mqttClient != nil && mqttClient.IsConnected() {
		token := mqttClient.Publish(topic, 1, false, payload)
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTTY] 发布状态消息失败: %v", token.Error())
		}
	}
}

// 全局变量用于跟踪活跃的转发会话
var (
	forwardingSessions = make(map[string]chan struct{})
	forwardingMutex    sync.Mutex
)

// 转发会话输出
func forwardSessionOutput(topicPrefix, sessionID string, manager *SessionManager) {

	// 检查是否已经有转发进程在运行
	forwardingMutex.Lock()
	if stopCh, exists := forwardingSessions[sessionID]; exists {
		// 通知现有转发进程停止
		close(stopCh)
		delete(forwardingSessions, sessionID)
	}

	// 创建新的停止通道
	stopCh := make(chan struct{})
	forwardingSessions[sessionID] = stopCh
	forwardingMutex.Unlock()

	// 在函数结束时清除通道
	defer func() {
		forwardingMutex.Lock()
		delete(forwardingSessions, sessionID)
		forwardingMutex.Unlock()
	}()

	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 无法获取会话来转发输出: %v", err)
		return
	}

	topic := fmt.Sprintf("%s/%s/%s", topicPrefix, sessionID, TopicOutput)

	// 打印输出主题
	log.Printf("[MQTTY] 转发输出到主题: %s", topic)

	// 获取Agent UUID用于前端响应主题
	agentUuid := config.GetAppConfig().UUID
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)

	for {
		select {
		case output, ok := <-session.Output:
			if !ok {
				// 通道已关闭
				return
			}

			// 打印输出的前30个字符以便于调试
			preview := string(output)
			if len(preview) > 30 {
				preview = preview[:30] + "..."
			}
			log.Printf("[MQTTY] 收到会话输出 (%s): %q", sessionID, preview)

			// 创建标准MQTT消息
			message := Message{
				SessionID: sessionID,
				Type:      "output", // 添加类型信息，与前端对应
				Data:      string(output),
				Timestamp: time.Now().UnixNano() / 1e6,
			}

			payload, err := json.Marshal(message)
			if err != nil {
				log.Printf("[MQTTY] 序列化输出消息失败: %v", err)
				continue
			}

			// 只发送到前端响应主题，不再发送到传统输出主题
			// 已经确认前端只监听 response主题
			// 发布到前端响应主题
			if mqttClient != nil && mqttClient.IsConnected() {
				token := mqttClient.Publish(responseTopic, 1, false, payload)
				if token.Wait() && token.Error() != nil {
					log.Printf("[MQTTY] 发布输出消息失败: %v", token.Error())
				}
			}

		case <-session.Done:
			// 会话已关闭
			return

		case <-stopCh:
			// 收到停止信号，可能是新的转发进程启动
			log.Printf("[MQTTY] 终止已有的输出转发 (会话: %s)", sessionID)
			return
		}
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
