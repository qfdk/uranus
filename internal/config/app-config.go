package config

import (
	"crypto/rand"
	"encoding/base64"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
	"uranus/internal/tools"
)

type AppConfig struct {
	VhostPath     string `json:"vhostPath"`
	Email         string `json:"email"`
	SSLPath       string `json:"SSLPath"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	UUID          string `json:"uuid"`
	URL           string `json:"url"`
	ControlCenter string `json:"controlCenter"`
	Token         string `json:"token"`
	InstallPath   string `json:"installPath"`
	IP            string `json:"ip"`
	// MQTT配置
	MQTTBroker string `json:"mqttBroker"` // MQTT服务器地址
}

var (
	_appConfig *AppConfig
	configOnce sync.Once
	configLock sync.RWMutex
	ipCache    string
	ipCacheTTL time.Time
)

// GetConfigLock 获取配置锁，用于外部同步
func GetConfigLock() *sync.RWMutex {
	return &configLock
}

// GenerateSecureToken 创建一个加密安全的随机令牌
// 返回4字节（32位）随机数据的base64编码字符串
func GenerateSecureToken() string {
	// 创建一个字节切片来存储随机字节
	randomBytes := make([]byte, 4) // 32位熵

	// 使用随机数据填充字节切片
	_, err := rand.Read(randomBytes)
	if err != nil {
		// 记录错误但继续使用备用方案
		log.Printf("[-] 警告: 无法生成安全令牌: %v", err)
		return "myToken_" + uuid.New().String()[0:8] // UUID备用方案，截取8个字符
	}

	// 将随机字节编码为base64
	// 使用URLEncoding确保令牌是URL安全的
	token := base64.URLEncoding.EncodeToString(randomBytes)

	return token
}

// GetAppConfig returns the application configuration with thread-safe singleton pattern
func GetAppConfig() *AppConfig {
	configOnce.Do(func() {
		InitAppConfig()
	})

	// Use read lock for better concurrency
	configLock.RLock()
	defer configLock.RUnlock()

	return _appConfig
}

func loadConfig() {
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("[-] 读取配置文件失败: %v", err)
	}

	// Use write lock when updating configuration
	configLock.Lock()
	defer configLock.Unlock()

	_appConfig = &AppConfig{}
	_ = viper.Unmarshal(&_appConfig)
	// 自动生成缺失的UUID
	if _appConfig.UUID == "" {
		newUUID, _ := uuid.NewUUID()
		_appConfig.UUID = newUUID.String()
		viper.Set("uuid", _appConfig.UUID)
		_ = viper.WriteConfig()
	}

	// 自动获取IP地址
	if _appConfig.IP == "" {
		ip := getIP()
		if ip == "" {
			ip = "Unknown"
		}
		_appConfig.IP = ip
		viper.Set("ip", ip)
		_ = viper.WriteConfig()
	}
}

func InitAppConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")

	pwd := tools.GetPWD()
	viper.AddConfigPath(pwd)

	configFile := path.Join(pwd, "config.toml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {

		// 默认配置
		defaultConfig := map[string]interface{}{
			"url":           "http://" + getIP() + ":7777",
			"uuid":          uuid.New().String(),
			"token":         GenerateSecureToken(), // 使用我们的安全令牌生成器
			"vhostPath":     "/etc/nginx/conf.d",
			"sslpath":       "/etc/nginx/ssl",
			"email":         "hello@world.com",
			"username":      "admin",
			"password":      "admin",
			"installPath":   pwd,
			"controlCenter": "https://uranus-control.vercel.app",
			"ip":            getIP(),
			// 默认MQTT配置
			"mqttBroker": "mqtt://mqtt.qfdk.me:1883",
			//"mqttUsername": "",
			//"mqttPassword": "",
			//"mqttTopic":    "uranus",
		}

		// 设置所有默认值
		for key, value := range defaultConfig {
			viper.Set(key, value)
		}

		_ = viper.SafeWriteConfig()
	}

	loadConfig()

	// 设置配置监视器，带缓冲重新加载以防止过度重新加载
	viper.WatchConfig()

	// 防抖配置更改
	var configChangeTimer *time.Timer
	var configChangeMutex sync.Mutex

	viper.OnConfigChange(func(in fsnotify.Event) {
		configChangeMutex.Lock()
		defer configChangeMutex.Unlock()

		if configChangeTimer != nil {
			configChangeTimer.Stop()
		}

		// 将配置更改防抖至最多每秒一次
		configChangeTimer = time.AfterFunc(1*time.Second, func() {
			log.Println("[+] 配置文件已更新:", in.Name)

			configLock.Lock()
			_ = viper.Unmarshal(&_appConfig)
			configLock.Unlock()
		})
	})
}

// getIP retrieves the public IP with caching
func getIP() string {
	// Check if cache is valid
	if ipCache != "" && time.Now().Before(ipCacheTTL) {
		return ipCache
	}

	// Create an HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("GET", "https://ip.tar.tn", nil)
	if err != nil {
		log.Printf("请求创建失败: %v", err)
		return "Unknown"
	}
	req.Header.Set("User-Agent", "Uranus")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("请求失败: %v", err)
		return "Unknown"
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("请求返回非200状态码: %d", resp.StatusCode)
		return "Unknown"
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取响应体失败: %v", err)
		return "Unknown"
	}

	ip := strings.TrimSpace(string(body))

	// Update cache
	ipCache = ip
	ipCacheTTL = time.Now().Add(24 * time.Hour) // Cache for 24 hours

	return ip
}

// ReloadConfig 强制重新加载配置
func ReloadConfig() {
	configLock.Lock()
	defer configLock.Unlock()
	
	// 重新读取配置文件
	err := viper.ReadInConfig()
	if err != nil {
		log.Printf("[-] 重新读取配置文件失败: %v", err)
		return
	}
	
	// 重新解析配置到AppConfig
	_appConfig = &AppConfig{}
	err = viper.Unmarshal(&_appConfig)
	if err != nil {
		log.Printf("[-] 重新解析配置失败: %v", err)
		return
	}
}
