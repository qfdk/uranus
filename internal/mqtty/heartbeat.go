package mqtty

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/shirou/gopsutil/v3/mem"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
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

// 缓存的心跳数据，防止频繁读取内存信息
var cachedHeartbeat atomic.Value // *HeartbeatData
var lastHeartbeatTime time.Time
var heartbeatMutex sync.Mutex

// StartHeartbeat 启动MQTT心跳服务
func StartHeartbeat(ctx context.Context) {
	log.Println("[MQTTY] 心跳服务启动")

	// 检查连接状态
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Printf("[MQTTY] 心跳服务检测到MQTT未连接")
	}

	// 创建定时器 - 5分钟发送一次心跳信息
	ticker := time.NewTicker(5 * time.Minute)
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

	// 构建心跳数据，使用缓存减少性能开销
	heartbeatMutex.Lock()

	// 检查缓存中心跳是否在 10 秒内
	use_cached := false
	if !lastHeartbeatTime.IsZero() && time.Since(lastHeartbeatTime) < 10*time.Second {
		if cached := cachedHeartbeat.Load(); cached != nil {
			use_cached = true
		}
	}

	// 如果需要新的心跳数据
	var heartbeat *HeartbeatData
	var err error

	if !use_cached {
		// 生成新的心跳数据
		heartbeat, err = buildHeartbeatData()
		if err != nil {
			heartbeatMutex.Unlock()
			log.Printf("[MQTTY] 构建心跳数据失败: %v", err)
			return
		}

		// 更新缓存和时间
		cachedHeartbeat.Store(heartbeat)
		lastHeartbeatTime = time.Now()
	} else {
		// 使用缓存的心跳数据，但更新时间戳
		cachedHB := cachedHeartbeat.Load().(*HeartbeatData)
		heartbeat = &HeartbeatData{} // 创建新对象，避免修改缓存的数据
		*heartbeat = *cachedHB       // 拷贝缓存的数据
		heartbeat.Timestamp = time.Now()
		heartbeat.ActiveTime = heartbeat.Timestamp.Format("2006-01-02 15:04:05")
	}
	heartbeatMutex.Unlock()

	// 预分配缓冲区
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	if err := encoder.Encode(heartbeat); err != nil {
		log.Printf("[MQTTY] 心跳数据序列化失败: %v", err)
		return
	}

	// 发布心跳消息
	token := mqttClient.Publish(HeartbeatTopic, 1, false, buffer.Bytes())
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 心跳发送失败: %v", token.Error())
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

// 预分配的JSON编码器和缓冲区
var statusBuffer bytes.Buffer
var statusBufferMutex sync.Mutex

// publishStatus 发布状态消息
func publishStatus(status string) {
	if mqttClient == nil || !mqttClient.IsConnected() {
		log.Printf("[MQTTY] 状态消息取消: MQTT未连接")
		return
	}

	// 锁定状态缓冲区以准备消息
	statusBufferMutex.Lock()
	defer statusBufferMutex.Unlock()

	// 重置缓冲区
	statusBuffer.Reset()

	// 准备状态消息参数
	timestampStr := time.Now().Format(time.RFC3339)

	// 直接写入JSON格式的状态消息，避免使用json.Marshal
	statusBuffer.WriteString("{\"uuid\":\"")
	statusBuffer.WriteString(getUUID())
	statusBuffer.WriteString("\",\"status\":\"")
	statusBuffer.WriteString(status)
	statusBuffer.WriteString("\",\"timestamp\":\"")
	statusBuffer.WriteString(timestampStr)
	statusBuffer.WriteString("\"}")

	// 发布状态消息
	token := mqttClient.Publish(StatusTopic, 1, true, statusBuffer.Bytes())
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 状态消息发送失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 已发送状态: %s", status)
	}
}
