package mqtty

import (
	"context"
	"encoding/json"
	"github.com/shirou/gopsutil/v3/mem"
	"log"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"
)

// MQTT主题定义
const (
	// HeartbeatTopic 心跳主题
	HeartbeatTopic = "uranus/heartbeat"
	// StatusTopic 状态主题
	StatusTopic = "uranus/status"
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

// StartHeartbeat 启动MQTT心跳服务
func StartHeartbeat(ctx context.Context) {
	log.Println("[MQTTY] 心跳服务启动")

	// 检查连接状态
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Printf("[MQTTY] 心跳服务检测到MQTT未连接")
	}

	// 创建定时器 - 5秒发送一次心跳信息
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 立即发送一次心跳
	sendHeartbeat()
	// 发送状态信息
	publishStatus("online")

	// 心跳循环
	for {
		select {
		case <-ticker.C:
			sendHeartbeat()
		case <-ctx.Done():
			log.Println("[MQTTY] 心跳服务停止")
			// 发布离线状态
			publishStatus("offline")
			return
		}
	}
}

// sendHeartbeat 发送MQTT心跳
func sendHeartbeat() {
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Println("[MQTTY] 心跳取消: MQTT未连接")
		return
	}

	// 构建心跳数据
	heartbeat, err := buildHeartbeatData()
	if err != nil {
		log.Printf("[MQTTY] 构建心跳数据失败: %v", err)
		return
	}

	// 序列化为JSON
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		log.Printf("[MQTTY] 心跳数据序列化失败: %v", err)
		return
	}

	// 发布心跳消息
	token := mqttClient.Publish(HeartbeatTopic, 1, false, payload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 心跳发送失败: %v", token.Error())
	}
	//else {
	//	log.Printf("[MQTTY] 心跳发送成功")
	//}
}

// buildHeartbeatData 构建心跳数据
func buildHeartbeatData() (*HeartbeatData, error) {
	appConfig := config.GetAppConfig()
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	currentTime := time.Now()
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &HeartbeatData{
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
	}, nil
}

// publishStatus 发布状态消息
func publishStatus(status string) {
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Printf("[MQTTY] 状态消息取消: MQTT未连接")
		return
	}

	// 准备状态消息
	appConfig := config.GetAppConfig()
	statusData := map[string]interface{}{
		"uuid":      appConfig.UUID,
		"status":    status,
		"timestamp": time.Now(),
	}

	// 序列化消息
	payload, err := json.Marshal(statusData)
	if err != nil {
		log.Printf("[MQTTY] 序列化状态消息失败: %v", err)
		return
	}

	// 发布状态消息
	token := mqttClient.Publish(StatusTopic, 1, true, payload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 状态消息发送失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 已发送状态: %s", status)
	}
}
