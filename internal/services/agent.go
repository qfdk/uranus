package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"

	"github.com/shirou/gopsutil/v3/mem"
)

// AgentData 代理数据结构
type AgentData struct {
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
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
}

// RegisterAgent 向控制中心注册代理
func RegisterAgent() {
	// 获取应用配置
	appConfig := config.GetAppConfig()
	controlCenter := appConfig.ControlCenter

	// 如果控制中心URL为空，则跳过注册
	if controlCenter == "" {
		log.Println("[Agent] 控制中心URL未配置，跳过注册")
		return
	}

	// 构建代理数据
	agentData, err := buildAgentData()
	if err != nil {
		log.Printf("[Agent] 构建代理数据失败: %v", err)
		return
	}

	// 序列化数据
	jsonData, err := json.Marshal(agentData)
	if err != nil {
		log.Printf("[Agent] 序列化代理数据失败: %v", err)
		return
	}

	// 构建请求URL
	endpoint := controlCenter
	if endpoint[len(endpoint)-1:] != "/" {
		endpoint += "/"
	}
	endpoint += "api/agents"

	// 发送POST请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[Agent] 创建请求失败: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Uranus-Agent")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Agent] 注册请求失败: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[Agent] 代理注册成功，状态码: %d", resp.StatusCode)
	} else {
		log.Printf("[Agent] 代理注册失败，状态码: %d", resp.StatusCode)
	}
}

// StartAgentHeartbeat 启动代理心跳服务
func StartAgentHeartbeat(ctx context.Context) {
	log.Println("[Agent] 控制中心心跳服务启动")

	// 创建定时器 - 15分钟发送一次心跳
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	// 立即发送一次心跳
	RegisterAgent()

	// 心跳循环
	for {
		select {
		case <-ticker.C:
			RegisterAgent()
		case <-ctx.Done():
			log.Println("[Agent] 控制中心心跳服务停止")

			// 发送离线状态
			setAgentOffline()
			return
		}
	}
}

// setAgentOffline 设置代理离线状态
func setAgentOffline() {
	// 获取应用配置
	appConfig := config.GetAppConfig()
	controlCenter := appConfig.ControlCenter

	// 如果控制中心URL为空，则跳过
	if controlCenter == "" {
		return
	}

	// 构建代理数据
	agentData, err := buildAgentData()
	if err != nil {
		log.Printf("[Agent] 构建代理数据失败: %v", err)
		return
	}

	// 设置为离线状态
	agentData.Status = "offline"

	// 序列化数据
	jsonData, err := json.Marshal(agentData)
	if err != nil {
		log.Printf("[Agent] 序列化代理数据失败: %v", err)
		return
	}

	// 构建请求URL
	endpoint := controlCenter
	if endpoint[len(endpoint)-1:] != "/" {
		endpoint += "/"
	}
	endpoint += "api/agents"

	// 发送POST请求
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Uranus-Agent")

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// buildAgentData 构建代理数据
func buildAgentData() (*AgentData, error) {
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

	return &AgentData{
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
		Status:       "online",
	}, nil
}
