package services

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"uranus/internal/config"

	"github.com/spf13/viper"
)

// UpdateAgentConfig 更新Agent配置文件，简单直接的方法
func UpdateAgentConfig(configData map[string]interface{}) ([]string, error) {
	var updatedKeys []string

	// 允许更新的配置字段
	allowedFields := map[string]string{
		"mqttBroker":    "mqttbroker",
		"email":         "email", 
		"username":      "username",
		"vhostPath":     "vhostpath",
		"sslPath":       "sslpath",
		"controlCenter": "controlcenter",
		"token":         "token",
		"password":      "password",
		"ip":            "ip",
		"url":           "url",
		"uuid":          "uuid",
		"installPath":   "installpath",
	}

	// 使用相同的锁确保与loadConfig不冲突
	configLock := config.GetConfigLock()
	configLock.Lock()
	defer configLock.Unlock()

	// 强制重新读取配置文件，确保viper内存中有完整配置
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("[CONFIG] 读取配置文件失败: %v", err)
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 简单更新配置值
	for key, value := range configData {
		if configKey, allowed := allowedFields[key]; allowed {
			if strValue, ok := value.(string); ok && strValue != "" {
				viper.Set(configKey, strValue)
				updatedKeys = append(updatedKeys, key)
			}
		}
	}

	if len(updatedKeys) == 0 {
		return nil, fmt.Errorf("没有有效的配置字段需要更新")
	}

	// 直接写入配置文件
	if err := viper.WriteConfig(); err != nil {
		log.Printf("[CONFIG] 写入配置失败: %v", err)
		return nil, fmt.Errorf("写入配置失败: %v", err)
	}

	log.Printf("[CONFIG] 配置已更新: %v", updatedKeys)
	return updatedKeys, nil
}

// RestartAgent 重启Agent服务
func RestartAgent() error {
	log.Printf("[SERVICE] 开始重启Agent...")

	// 尝试不同的重启方法
	methods := []struct {
		name    string
		command string
		args    []string
	}{
		{"systemctl", "systemctl", []string{"restart", "uranus.service"}},
		{"systemctl-daemon", "systemctl", []string{"daemon-reload"}},
		{"service", "service", []string{"uranus", "restart"}},
	}

	for _, method := range methods {
		log.Printf("[SERVICE] 尝试使用 %s 重启服务...", method.name)

		cmd := exec.Command(method.command, method.args...)
		output, err := cmd.CombinedOutput()

		if err == nil {
			log.Printf("[SERVICE] 使用 %s 重启成功", method.name)
			return nil
		}

		log.Printf("[SERVICE] %s 重启失败: %v, 输出: %s", method.name, err, string(output))
	}

	// 如果所有方法都失败，尝试发送信号给自己
	log.Printf("[SERVICE] 所有重启方法都失败，尝试优雅退出...")

	// 获取当前进程ID
	pid := os.Getpid()
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("找不到当前进程: %v", err)
	}

	// 发送SIGUSR2信号触发优雅重启
	if err := process.Signal(syscall.SIGUSR2); err != nil {
		return fmt.Errorf("发送重启信号失败: %v", err)
	}

	log.Printf("[SERVICE] 已发送重启信号")

	// 延迟触发Agent注册，确保重启后配置生效
	go func() {
		time.Sleep(3 * time.Second) // 等待重启完成
		RegisterAgent()
	}()

	return nil
}

// RefreshAgentIP 刷新Agent的IP地址并更新配置文件
func RefreshAgentIP() (string, error) {
	log.Printf("[CONFIG] 开始刷新IP地址...")

	// 获取新的IP地址
	newIP, err := getCurrentIP()
	if err != nil {
		return "", fmt.Errorf("获取IP地址失败: %v", err)
	}

	log.Printf("[CONFIG] 获取到新IP地址: %s", newIP)

	// 简单直接更新IP
	configData := map[string]interface{}{
		"ip": newIP,
	}
	
	_, err = UpdateAgentConfig(configData)
	if err != nil {
		return "", err
	}

	log.Printf("[CONFIG] IP地址已更新到配置文件: %s", newIP)

	// 立即触发Agent注册，更新控制中心的IP信息
	go func() {
		time.Sleep(1 * time.Second) // 等待配置生效
		RegisterAgent()
	}()

	return newIP, nil
}

// getCurrentIP 获取当前的公网IP地址
func getCurrentIP() (string, error) {
	// 尝试多个IP检测服务，优先使用更稳定的服务
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
		"https://ipinfo.io/ip",
		"https://icanhazip.com",
		"https://ipecho.net/plain",
	}

	// 创建HTTP客户端，设置超时和User-Agent
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("[CONFIG] 创建请求失败 %s: %v", url, err)
			continue
		}

		// 设置User-Agent避免被某些服务阻止
		req.Header.Set("User-Agent", "Uranus-Agent/1.0")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[CONFIG] 从 %s 获取IP失败: %v", url, err)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			log.Printf("[CONFIG] 从 %s 获取IP失败，状态码: %d", url, resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("[CONFIG] 读取 %s 响应失败: %v", url, err)
			continue
		}

		ip := strings.TrimSpace(string(body))

		// 更严格的IP格式验证
		if isValidIPv4(ip) {
			log.Printf("[CONFIG] 从 %s 获取到IP: %s", url, ip)
			return ip, nil
		}

		// 如果返回的内容太长，可能是HTML页面，只记录前100个字符
		if len(ip) > 100 {
			ip = ip[:100] + "..."
		}
		log.Printf("[CONFIG] 从 %s 获取到无效IP: %s", url, ip)
	}

	return "", fmt.Errorf("所有IP检测服务都失败")
}

// isValidIPv4 验证IPv4地址格式
func isValidIPv4(ip string) bool {
	if len(ip) < 7 || len(ip) > 15 {
		return false
	}

	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}

	for _, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return false
		}

		// 检查是否全是数字
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}

		// 检查数值范围
		num := 0
		for _, char := range part {
			num = num*10 + int(char-'0')
		}
		if num > 255 {
			return false
		}

		// 检查前导零（除了单独的0）
		if len(part) > 1 && part[0] == '0' {
			return false
		}
	}

	return true
}

// copyConfigFile 复制配置文件，用于备份和恢复
func copyConfigFile(src, dst string) error {
	// 读取源文件
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取源文件失败: %v", err)
	}
	
	// 写入目标文件
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("写入目标文件失败: %v", err)
	}
	
	return nil
}

