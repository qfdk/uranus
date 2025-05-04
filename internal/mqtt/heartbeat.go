// internal/mqtt/heartbeat.go
package mqtt

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

// StartHeartbeat 启动MQTT心跳
func StartHeartbeat(ctx context.Context) {
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
	sendHeartbeat()

	// 心跳循环
	for {
		select {
		case <-ticker.C:
			sendHeartbeat()
		case <-ctx.Done():
			log.Println("[MQTT] 心跳服务停止")
			// 发布离线状态并断开连接
			Disconnect()
			return
		}
	}
}

// sendHeartbeat 发送MQTT心跳
func sendHeartbeat() {
	if !IsConnected() {
		log.Println("[MQTT] 心跳取消: MQTT未连接")
		return
	}

	// 构建心跳数据
	heartbeat, err := buildHeartbeatData()
	if err != nil {
		log.Printf("[MQTT] 构建心跳数据失败: %v", err)
		return
	}

	// 序列化为JSON
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		log.Printf("[MQTT] 心跳数据序列化失败: %v", err)
		return
	}

	// 发布心跳消息
	err = Publish(HeartbeatTopic, payload)
	if err != nil {
		log.Printf("[MQTT] 心跳发送失败: %v", err)
	}
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
	if !IsConnected() {
		return
	}

	payload, err := createStatusMessage(status)
	if err != nil {
		log.Printf("[MQTT] 创建状态消息失败: %v", err)
		return
	}

	err = Publish(StatusTopic, payload)
	if err != nil {
		log.Printf("[MQTT] 状态消息发送失败: %v", err)
	}
}
