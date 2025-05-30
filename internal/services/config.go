package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/viper"
)

// UpdateAgentConfig 更新Agent配置文件
func UpdateAgentConfig(configData map[string]interface{}) ([]string, error) {
	log.Printf("[CONFIG] 开始更新配置: %+v", configData)

	var updatedKeys []string

	// 允许更新的配置字段 (前端字段名 -> 配置文件字段名)
	allowedFields := map[string]string{
		"mqttBroker":    "mqttBroker",
		"email":         "email",
		"username":      "username",
		"vhostPath":     "vhostpath",     // 配置文件中是小写
		"sslPath":       "sslpath",      // 配置文件中是小写
		"controlCenter": "controlcenter", // 配置文件中是小写
		"token":         "token",
	}

	// 更新配置值
	for key, value := range configData {
		if configKey, allowed := allowedFields[key]; allowed {
			if strValue, ok := value.(string); ok && strValue != "" {
				viper.Set(configKey, strValue)
				updatedKeys = append(updatedKeys, key)
				log.Printf("[CONFIG] 更新配置 %s = %s", configKey, strValue)
			}
		} else {
			log.Printf("[CONFIG] 跳过不允许的配置字段: %s", key)
		}
	}

	if len(updatedKeys) == 0 {
		return nil, fmt.Errorf("没有有效的配置字段需要更新")
	}

	// 调试：显示viper的配置文件路径
	log.Printf("[CONFIG] 配置文件路径: %s", viper.ConfigFileUsed())
	
	// 调试：显示当前所有配置
	allSettings := viper.AllSettings()
	log.Printf("[CONFIG] 更新前的所有配置: %+v", allSettings)

	// 保存配置文件
	if err := viper.WriteConfig(); err != nil {
		log.Printf("[CONFIG] 保存配置文件失败: %v", err)
		return nil, fmt.Errorf("保存配置文件失败: %v", err)
	}

	// 调试：显示更新后的配置
	allSettingsAfter := viper.AllSettings()
	log.Printf("[CONFIG] 更新后的所有配置: %+v", allSettingsAfter)

	log.Printf("[CONFIG] 配置文件已更新，更新的字段: %v", updatedKeys)
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

	var _ error
	for _, method := range methods {
		log.Printf("[SERVICE] 尝试使用 %s 重启服务...", method.name)

		cmd := exec.Command(method.command, method.args...)
		output, err := cmd.CombinedOutput()

		if err == nil {
			log.Printf("[SERVICE] 使用 %s 重启成功", method.name)
			return nil
		}

		log.Printf("[SERVICE] %s 重启失败: %v, 输出: %s", method.name, err, string(output))
		_ = err
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

	// 更新配置文件中的IP地址
	viper.Set("ip", newIP)

	// 保存配置文件
	if err := viper.WriteConfig(); err != nil {
		log.Printf("[CONFIG] 保存IP地址到配置文件失败: %v", err)
		return "", fmt.Errorf("保存配置文件失败: %v", err)
	}

	log.Printf("[CONFIG] IP地址已更新到配置文件: %s", newIP)
	return newIP, nil
}

// getCurrentIP 获取当前的公网IP地址
func getCurrentIP() (string, error) {
	// 尝试多个IP检测服务
	urls := []string{
		"https://ipinfo.io/ip",
		"https://api.ipify.org",
		"https://icanhazip.com",
		"https://ipecho.net/plain",
	}

	for _, url := range urls {
		cmd := exec.Command("curl", "-s", "--connect-timeout", "5", url)
		output, err := cmd.Output()
		if err != nil {
			log.Printf("[CONFIG] 从 %s 获取IP失败: %v", url, err)
			continue
		}

		ip := strings.TrimSpace(string(output))
		// 验证IP格式（简单检查）
		if len(ip) > 7 && len(ip) < 16 && strings.Count(ip, ".") == 3 {
			log.Printf("[CONFIG] 从 %s 获取到IP: %s", url, ip)
			return ip, nil
		}
		log.Printf("[CONFIG] 从 %s 获取到无效IP: %s", url, ip)
	}

	return "", fmt.Errorf("所有IP检测服务都失败")
}
