// internal/mqtt/client.go
package mqtt

import (
	"fmt"
	"log"
	"time"
	"uranus/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	// MQTT客户端设置
	MQTTKeepAlive   = 60    // 保持连接时间(秒)
	MQTTQoS         = 1     // 服务质量(0, 1, 2)
	MQTTRetained    = false // 是否保留消息
	MQTTConnTimeout = 10    // 连接超时(秒)
)

var (
	mqttClient    mqtt.Client // MQTT客户端实例
	mqttConnected bool        // MQTT连接状态
)

// InitMQTT 初始化MQTT客户端
func InitMQTT() error {
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
	opts.SetMaxReconnectInterval(5 * time.Minute)

	// 设置遗嘱消息 (当客户端异常断开时发送)
	willMessage, _ := createStatusMessage("offline")
	opts.SetWill(StatusTopic, string(willMessage), byte(MQTTQoS), true)

	// 连接回调
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("[MQTT] 连接成功")
		mqttConnected = true

		// 订阅命令主题
		appConfig := config.GetAppConfig()
		commandTopic := CommandTopic + appConfig.UUID
		token := client.Subscribe(commandTopic, byte(MQTTQoS), handleCommand)
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTT] 订阅命令主题失败: %v", token.Error())
		} else {
			log.Printf("[MQTT] 已订阅命令主题: %s", commandTopic)
		}

		// 发布上线状态消息
		publishStatus("online")
	})

	// 连接丢失回调
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("[MQTT] 连接丢失: %v", err)
		mqttConnected = false
	})

	// 创建客户端
	mqttClient = mqtt.NewClient(opts)

	// 连接到MQTT broker
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT连接失败: %v", token.Error())
	}

	return nil
}

// IsConnected 返回MQTT连接状态
func IsConnected() bool {
	return mqttConnected
}

// Publish 发布消息到指定主题
func Publish(topic string, payload []byte) error {
	if !mqttConnected {
		return fmt.Errorf("MQTT未连接")
	}

	token := mqttClient.Publish(topic, byte(MQTTQoS), MQTTRetained, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("消息发布失败: %v", token.Error())
	}

	return nil
}

// Disconnect 断开MQTT连接
func Disconnect() {
	if mqttClient != nil && mqttClient.IsConnected() {
		publishStatus("offline")
		mqttClient.Disconnect(250)
	}
}

// GetClient 获取MQTT客户端实例
func GetClient() mqtt.Client {
	return mqttClient
}
