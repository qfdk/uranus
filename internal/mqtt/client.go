// internal/mqtt/client.go
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"uranus/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	// MQTT客户端设置
	MQTTKeepAlive   = 30    // 保持连接时间(秒)，从60秒减少到30秒
	MQTTQoS         = 1     // 服务质量(0, 1, 2)
	MQTTRetained    = false // 是否保留消息
	MQTTConnTimeout = 15    // 连接超时(秒)，从10秒增加到15秒
	MQTTMaxRetries  = 3     // 发布消息最大重试次数
)

var (
	mqttClient    mqtt.Client // MQTT客户端实例
	mqttConnected bool        // MQTT连接状态
)

// InitMQTT 初始化MQTT客户端
func InitMQTT() error {

	// 如果客户端已存在且已连接，直接返回成功
	if mqttClient != nil && mqttClient.IsConnected() {
		mqttConnected = true
		return nil
	}

	// 获取应用配置
	appConfig := config.GetAppConfig()

	// 创建MQTT连接选项
	opts := mqtt.NewClientOptions()

	// 设置MQTT broker地址
	mqttBroker := appConfig.MQTTBroker
	if mqttBroker == "" {
		mqttBroker = "mqtt://mqtt.qfdk.me:1883" // 默认MQTT服务器地址
	}
	opts.AddBroker(mqttBroker)

	// 设置客户端ID (使用UUID确保唯一性)
	clientID := fmt.Sprintf("uranus-%s", appConfig.UUID)
	opts.SetClientID(clientID)

	// 设置保持连接时间
	opts.SetKeepAlive(MQTTKeepAlive * time.Second)

	// 设置连接超时
	opts.SetConnectTimeout(MQTTConnTimeout * time.Second)

	// 设置自动重连
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second) // 减少最大重连间隔
	opts.SetCleanSession(false)                    // 使用持久会话保留订阅

	// 设置遗嘱消息 (当客户端异常断开时发送)
	willMessage, _ := createStatusMessage("offline")
	opts.SetWill(StatusTopic, string(willMessage), byte(MQTTQoS), true)

	// 连接回调
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("[MQTT] 连接成功")

		// 设置连接状态标志
		mqttConnected = true

		// 发布上线状态消息
		publishStatus("online")

		// 订阅命令主题
		appConfig := config.GetAppConfig()
		commandTopic := CommandTopic + appConfig.UUID
		token := client.Subscribe(commandTopic, byte(MQTTQoS), handleCommand)
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTT] 订阅命令主题失败: %v", token.Error())
		} else {
			log.Printf("[MQTT] 已订阅命令主题: %s", commandTopic)
		}
	})

	// 连接丢失回调
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("[MQTT] 连接丢失: %v", err)
		mqttConnected = false

		// 记录尝试重连的消息，实际重连由自动重连机制处理
		log.Println("[MQTT] 自动重连机制将尝试重新建立连接...")
	})

	// 重连中回调
	opts.SetReconnectingHandler(func(client mqtt.Client, opts *mqtt.ClientOptions) {
		log.Println("[MQTT] 正在尝试重新连接...")
	})

	// 创建客户端
	mqttClient = mqtt.NewClient(opts)

	// 连接到MQTT broker
	log.Println("[MQTT] 正在连接到服务器...")
	token := mqttClient.Connect()
	if !token.WaitTimeout(MQTTConnTimeout * time.Second) {
		return fmt.Errorf("MQTT连接超时")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("MQTT连接失败: %v", err)
	}

	// 确认连接状态
	if !mqttClient.IsConnected() {
		return fmt.Errorf("MQTT连接建立失败，客户端报告未连接状态")
	}

	log.Println("[MQTT] 连接已成功建立")
	return nil
}

// IsConnected 返回MQTT连接状态
func IsConnected() bool {
	// 同时检查标志和客户端状态
	return mqttConnected && mqttClient != nil && mqttClient.IsConnected()
}

// Publish 发布消息到指定主题
func Publish(topic string, payload []byte) error {
	// 首先验证连接状态
	if mqttClient == nil {
		return fmt.Errorf("MQTT客户端未初始化")
	}

	// 检查客户端连接状态并尝试重新连接
	if !mqttClient.IsConnected() {
		log.Println("[MQTT] 发布消息前检测到连接断开，尝试重新连接")
		mqttConnected = false

		// 尝试重新初始化连接
		if err := InitMQTT(); err != nil {
			return fmt.Errorf("重新连接失败: %v", err)
		}
	}

	// 实现消息发布重试逻辑
	var lastErr error
	for i := 0; i < MQTTMaxRetries; i++ {
		// 如果不是第一次尝试，记录重试信息
		if i > 0 {
			log.Printf("[MQTT] 正在重试发布消息(尝试 %d/%d)", i+1, MQTTMaxRetries)
		}

		token := mqttClient.Publish(topic, byte(MQTTQoS), MQTTRetained, payload)
		if token.WaitTimeout(5*time.Second) && token.Error() != nil {
			lastErr = token.Error()
			log.Printf("[MQTT] 消息发布失败: %v", lastErr)
			time.Sleep(500 * time.Millisecond) // 重试前短暂等待
			continue
		}

		// 发布成功
		return nil
	}

	// 所有重试都失败
	return fmt.Errorf("消息发布失败: %v (已重试%d次)", lastErr, MQTTMaxRetries)
}

// Disconnect 断开MQTT连接
func Disconnect() {
	if mqttClient != nil && mqttClient.IsConnected() {
		// 先发布离线状态
		publishStatusInternal("offline")

		// 断开连接，合理的超时时间
		mqttClient.Disconnect(250)
		log.Println("[MQTT] 已断开连接")
	}

	mqttConnected = false
}

// GetClient 获取MQTT客户端实例
func GetClient() mqtt.Client {
	return mqttClient
}

// 内部状态发布函数，不加锁版本
func publishStatusInternal(status string) {
	// 不通过Publish函数以避免死锁，直接使用客户端发布
	if mqttClient != nil && mqttClient.IsConnected() {
		payload, err := createStatusMessage(status)
		if err == nil {
			token := mqttClient.Publish(StatusTopic, byte(MQTTQoS), true, payload)
			token.Wait()
			if token.Error() != nil {
				log.Printf("[MQTT] 状态消息直接发送失败: %v", token.Error())
			}
		}
	}
}

// 创建状态消息的辅助函数
func createStatusMessage(status string) ([]byte, error) {
	appConfig := config.GetAppConfig()
	statusData := map[string]interface{}{
		"uuid":      appConfig.UUID,
		"status":    status,
		"timestamp": time.Now(),
	}

	return json.Marshal(statusData)
}
