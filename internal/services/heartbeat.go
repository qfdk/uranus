package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/mem"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"
)

// HeartbeatWithContext runs heartbeat service with context support for graceful shutdown
func HeartbeatWithContext(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Create an HTTP client that can be reused across requests
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	for {
		select {
		case <-ticker.C:
			go sendHeartbeat(client)
		case <-ctx.Done():
			log.Println("[Heartbeat] Service stopping due to context cancellation")
			return
		}
	}
}

// Heartbeat for backward compatibility
func Heartbeat() {
	log.Println("[Heartbeat] 心跳包初始化")
	HeartbeatWithContext(context.Background())
}

func sendHeartbeat(client *http.Client) {
	if gin.Mode() != gin.ReleaseMode {
		log.Println("[Heartbeat] 发送心跳包")
	}

	hostname, _ := os.Hostname()
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		fmt.Println("获取内存信息失败:", err)
		return
	}

	data := gin.H{
		"uuid": config.GetAppConfig().UUID,

		// 构建信息
		"buildTime":    config.BuildTime,
		"buildVersion": config.BuildVersion,
		"commitId":     config.CommitID,
		"goVersion":    runtime.Version(),
		"version":      config.BuildVersion,
		//系统信息
		"hostname":   hostname,
		"ip":         config.GetAppConfig().IP,
		"os":         runtime.GOOS,
		"memory":     tools.FormatBytes(vmStat.Total),
		"url":        config.GetAppConfig().URL,
		"token":      config.GetAppConfig().Token,
		"activeTime": time.Now().Format("2006-01-02 15:04:05"),
	}

	bytesData, err := json.Marshal(data)
	if err != nil {
		log.Println("[Heartbeat] Error marshaling JSON:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", config.GetAppConfig().ControlCenter+"/api/agents", bytes.NewReader(bytesData))
	if err != nil {
		log.Println("[Heartbeat] Error creating request:", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Only log at debug level to avoid cluttering logs with connection errors
		if gin.Mode() == gin.DebugMode {
			log.Println("[Heartbeat] Error sending heartbeat:", err)
		}
		return
	}
	defer resp.Body.Close()
}
