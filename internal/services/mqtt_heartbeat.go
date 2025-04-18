// internal/services/mqtt_heartbeat.go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/mem"
	"log"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	// MQTT主题定义
	HeartbeatTopic = "uranus/heartbeat" // 心跳消息主题
	CommandTopic   = "uranus/command/"  // 命令主题前缀，会拼接UUID
	ResponseTopic  = "uranus/response/" // 响应主题前缀，会拼接UUID
	StatusTopic    = "uranus/status"    // 全局状态主题

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

// HeartbeatData 心跳数据结构
type HeartbeatData struct {
	UUID string `json:"uuid"`
	// 构建信息
	BuildTime    string `json:"buildTime"`
	BuildVersion string `json:"buildVersion"`
	CommitID     string `json:"commitId"`
	GoVersion    string `json:"goVersion"`
	// 系统信息
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	Memory   string `json:"memory"`
	URL      string `json:"url"`
	Token    string `json:"token"`
	// 心跳信息
	Timestamp  time.Time `json:"timestamp"`
	ActiveTime string    `json:"activeTime"`
}

// CommandMessage 命令消息结构
type CommandMessage struct {
	Command   string            `json:"command"`          // 命令类型: reload, restart, stop, etc.
	Params    map[string]string `json:"params,omitempty"` // 可选参数
	RequestID string            `json:"requestId"`        // 请求ID，用于匹配响应
}

// ResponseMessage 响应消息结构
type ResponseMessage struct {
	Command   string `json:"command"`        // 对应的命令
	RequestID string `json:"requestId"`      // 对应的请求ID
	Success   bool   `json:"success"`        // 是否成功
	Message   string `json:"message"`        // 响应消息
	Data      any    `json:"data,omitempty"` // 可选的返回数据
}

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
	willMessage, _ := json.Marshal(map[string]interface{}{
		"uuid":      appConfig.UUID,
		"status":    "offline",
		"timestamp": time.Now(),
	})
	opts.SetWill(StatusTopic, string(willMessage), byte(MQTTQoS), true)

	// 连接回调
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("[MQTT] 连接成功")
		mqttConnected = true

		// 订阅命令主题
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

// StartMQTTHeartbeat 启动MQTT心跳
func StartMQTTHeartbeatWithContext(ctx context.Context) {
	log.Println("[MQTT] 心跳服务启动")

	// 初始化MQTT
	if err := InitMQTT(); err != nil {
		log.Printf("[MQTT] 初始化失败: %v", err)
		return
	}

	// 创建定时器
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 立即发送一次心跳
	_sendHeartbeat()

	// 心跳循环
	for {
		select {
		case <-ticker.C:
			_sendHeartbeat()
		case <-ctx.Done():
			log.Println("[MQTT] 心跳服务停止")
			// 发布离线状态
			publishStatus("offline")
			// 断开MQTT连接
			mqttClient.Disconnect(250)
			return
		}
	}
}

// StartMQTTHeartbeat 向后兼容的函数
func StartMQTTHeartbeat() {
	StartMQTTHeartbeatWithContext(context.Background())
}

// _sendHeartbeat 发送MQTT心跳
func _sendHeartbeat() {
	if !mqttConnected {
		log.Println("[MQTT] 心跳取消: MQTT未连接")
		return
	}

	appConfig := config.GetAppConfig()
	hostname, _ := os.Hostname()
	currentTime := time.Now()
	vmStat, err := mem.VirtualMemory()

	// 构建心跳数据
	heartbeat := HeartbeatData{
		UUID:         appConfig.UUID,
		BuildTime:    config.BuildTime,
		BuildVersion: config.BuildVersion,
		CommitID:     config.CommitID,
		GoVersion:    config.GoVersion,
		Hostname:     hostname,
		IP:           appConfig.IP,
		OS:           runtime.GOOS,
		Memory:       tools.FormatBytes(vmStat.Total),
		URL:          appConfig.URL,
		Token:        appConfig.Token,
		Timestamp:    currentTime,
		ActiveTime:   currentTime.Format("2006-01-02 15:04:05"),
	}

	// 序列化为JSON
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		log.Printf("[MQTT] 心跳数据序列化失败: %v", err)
		return
	}

	// 发布心跳消息
	token := mqttClient.Publish(HeartbeatTopic, byte(MQTTQoS), MQTTRetained, payload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTT] 心跳发送失败: %v", token.Error())
	} else {
		if gin.Mode() == gin.DebugMode {
			log.Println("[MQTT] 心跳发送成功")
		}
	}
}

// publishStatus 发布状态消息
func publishStatus(status string) {
	if !mqttConnected {
		return
	}

	appConfig := config.GetAppConfig()
	statusData := map[string]interface{}{
		"uuid":      appConfig.UUID,
		"status":    status,
		"timestamp": time.Now(),
	}

	payload, _ := json.Marshal(statusData)
	mqttClient.Publish(StatusTopic, byte(MQTTQoS), true, payload)
}

// handleCommand 处理接收到的命令
func handleCommand(client mqtt.Client, msg mqtt.Message) {
	log.Printf("[MQTT] 收到命令: %s", string(msg.Payload()))

	// 解析命令
	var command CommandMessage
	if err := json.Unmarshal(msg.Payload(), &command); err != nil {
		log.Printf("[MQTT] 命令解析失败: %v", err)
		return
	}

	// 准备响应
	response := ResponseMessage{
		Command:   command.Command,
		RequestID: command.RequestID,
		Success:   true,
		Message:   "OK",
	}

	// 根据命令类型执行相应操作
	switch command.Command {
	case "reload":
		result := ReloadNginx()
		response.Message = result

	case "restart":
		result := StopNginx()
		if result == "OK" {
			result = StartNginx()
		}
		response.Message = result

	case "stop":
		result := StopNginx()
		response.Message = result

	case "start":
		result := StartNginx()
		response.Message = result

	case "update":
		// 异步执行更新操作
		go func() {
			updateUrl := "https://fr.qfdk.me/uranus/uranus-" + runtime.GOARCH
			if url, ok := command.Params["url"]; ok && url != "" {
				updateUrl = url
			}

			err := ToUpdateProgram(updateUrl)
			if err != nil {
				log.Printf("[MQTT] 更新失败: %v", err)
			}
		}()
		response.Message = "更新操作已开始执行"

	case "status":
		// 返回Nginx状态
		response.Message = NginxStatus()
		response.Data = map[string]interface{}{
			"nginx": NginxStatus() != "KO",
		}

	default:
		response.Success = false
		response.Message = "未知命令"
	}

	// 发送响应
	appConfig := config.GetAppConfig()
	responseTopic := ResponseTopic + appConfig.UUID

	payload, _ := json.Marshal(response)
	token := mqttClient.Publish(responseTopic, byte(MQTTQoS), false, payload)

	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTT] 响应发送失败: %v", token.Error())
	}
}
