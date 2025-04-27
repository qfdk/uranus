package mqtty

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

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

		// 创建会话
		err := manager.CreateSession(sessionID, shell)
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
	resizeData, ok := msg.Data.(map[string]interface{})
	if !ok {
		log.Printf("[MQTTY] 无效的调整大小数据: %T", msg.Data)
		return
	}

	rows, rowsOK := resizeData["rows"].(float64)
	cols, colsOK := resizeData["cols"].(float64)

	if !rowsOK || !colsOK {
		log.Printf("[MQTTY] 缺少行列数据: rows=%v, cols=%v", resizeData["rows"], resizeData["cols"])
		return
	}

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

// 转发会话输出
func forwardSessionOutput(topicPrefix, sessionID string, manager *SessionManager) {
	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 无法获取会话来转发输出: %v", err)
		return
	}

	topic := fmt.Sprintf("%s/%s/%s", topicPrefix, sessionID, TopicOutput)

	for {
		select {
		case output, ok := <-session.Output:
			if !ok {
				// 通道已关闭
				return
			}

			message := Message{
				SessionID: sessionID,
				Data:      string(output),
				Timestamp: time.Now().UnixNano() / 1e6,
			}

			payload, err := json.Marshal(message)
			if err != nil {
				log.Printf("[MQTTY] 序列化输出消息失败: %v", err)
				continue
			}

			if mqttClient != nil && mqttClient.IsConnected() {
				token := mqttClient.Publish(topic, 1, false, payload)
				if token.Wait() && token.Error() != nil {
					log.Printf("[MQTTY] 发布输出消息失败: %v", token.Error())
				}
			}

		case <-session.Done:
			// 会话已关闭
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
