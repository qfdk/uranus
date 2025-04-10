package services

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
)

// HeartbeatWithContext runs heartbeat service with context support for graceful shutdown
func HeartbeatWithContext(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Create an HTTP client that can be reused across requests
	client := &http.Client{
		Timeout: 5 * time.Second,
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
	HeartbeatWithContext(context.Background())
}

func sendHeartbeat(client *http.Client) {
	if gin.Mode() != gin.ReleaseMode {
		return // Only send heartbeats in release mode
	}

	hostname, _ := os.Hostname()
	data := gin.H{
		"buildTime":    config.BuildTime,
		"buildVersion": config.BuildVersion,
		"commitId":     config.CommitID,
		"goVersion":    runtime.Version(),
		"os":           runtime.GOOS,
		"url":          config.GetAppConfig().URL,
		"uuid":         config.GetAppConfig().UUID,
		"token":        config.GetAppConfig().Token,
		"ip":           config.GetAppConfig().IP,
		"hostname":     hostname,
		"activeTime":   time.Now().Format("2006-01-02 15:04:05"),
	}

	bytesData, err := json.Marshal(data)
	if err != nil {
		log.Println("[Heartbeat] Error marshaling JSON:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", config.GetAppConfig().ControlCenter, bytes.NewReader(bytesData))
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
