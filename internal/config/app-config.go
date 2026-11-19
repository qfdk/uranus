package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
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
	// 会话 cookie 的签名密钥，首次启动时随机生成
	SessionSecret string `json:"sessionSecret"`
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

// generateInitialPassword 生成首次启动使用的随机管理员密码（16 字节熵，20 个字符）
func generateInitialPassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("[-] 无法生成初始管理员密码: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)[:20]
}

// GenerateSecureToken 创建一个加密安全的随机令牌（128 位熵，URL 安全的 base64）。
// token 是远程命令、远程升级和配置下发的唯一凭证，也用作命令加密的密钥材料，熵不能太低。
func GenerateSecureToken() string {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		log.Fatalf("[-] 无法生成安全令牌: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes)
}

// generateSessionSecret 生成会话 cookie 的签名密钥（256 位）
func generateSessionSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("[-] 无法生成会话密钥: %v", err)
	}
	return hex.EncodeToString(b)
}

// GetAppConfig 返回当前配置快照。快照创建后不再修改，配置变化时整体替换。
func GetAppConfig() *AppConfig {
	InitAppConfig()

	configLock.RLock()
	defer configLock.RUnlock()

	return _appConfig
}

func loadConfig() {
	// 全局 viper 不是并发安全的，所有读写都在 configLock 下进行
	configLock.Lock()
	defer configLock.Unlock()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("[-] 读取配置文件失败: %v", err)
	}

	cfg := &AppConfig{}
	_ = viper.Unmarshal(cfg)
	// 自动生成缺失的UUID
	if cfg.UUID == "" {
		newUUID, _ := uuid.NewUUID()
		cfg.UUID = newUUID.String()
		viper.Set("uuid", cfg.UUID)
		_ = viper.WriteConfig()
	}

	// 自动生成缺失的会话密钥（旧版本使用固定密钥，升级后所有人需要重新登录一次）
	if cfg.SessionSecret == "" {
		cfg.SessionSecret = generateSessionSecret()
		viper.Set("sessionsecret", cfg.SessionSecret)
		_ = viper.WriteConfig()
	}

	// 自动获取IP地址
	if cfg.IP == "" {
		ip := getIP()
		if ip == "" {
			ip = "Unknown"
		}
		cfg.IP = ip
		viper.Set("ip", ip)
		_ = viper.WriteConfig()
	}

	_appConfig = cfg
}

// InitAppConfig 加载配置并开始监听配置文件变化，多次调用只会执行一次
func InitAppConfig() {
	configOnce.Do(initAppConfig)
}

func initAppConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	// 配置里有管理员密码、token 和会话密钥，只允许属主读写
	viper.SetConfigPermissions(0o600)

	pwd := tools.GetPWD()
	viper.AddConfigPath(pwd)

	configFile := path.Join(pwd, "config.toml")
	if info, err := os.Stat(configFile); err == nil {
		// 旧版本以 0644 创建了配置文件，收紧为仅属主可读写
		if info.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(configFile, 0o600); err != nil {
				log.Printf("[-] 无法收紧配置文件权限: %v", err)
			}
		}
	} else if os.IsNotExist(err) {

		// 首次启动生成随机管理员密码，只打印一次
		initialPassword := generateInitialPassword()
		log.Printf("[安全] 已生成初始管理员密码: %s （请登录后立即修改，或编辑 config.toml）", initialPassword)

		// 默认配置。不写 vhostPath 和 sslpath：网站目录取 nginx 主配置在 http 块里引用的目录，
		// 证书放在 nginx 配置目录下的 ssl，需要时可以在 config.toml 里指定这两项
		defaultConfig := map[string]interface{}{
			"url":           "http://" + getIP() + ":7777",
			"uuid":          uuid.New().String(),
			"token":         GenerateSecureToken(), // 使用我们的安全令牌生成器
			"email":         "hello@world.com",
			"username":      "admin",
			"password":      initialPassword,
			"installPath":   pwd,
			"controlCenter": "https://uranus-control.vercel.app",
			"ip":            getIP(),
			// 默认不启用 MQTT 远程管理；需要时配置自己的 broker（建议 mqtts:// + 账号 + ACL）
			"mqttBroker": "",
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
	watchConfigFile(configFile)
}

// watchConfigFile reloads the configuration when config.toml changes on disk.
// viper.WatchConfig is not used: it re-reads the file into the global viper
// from its own goroutine, outside configLock, racing with the config editor and
// remote config updates; it also stops watching once the file is replaced.
func watchConfigFile(configFile string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[-] 无法监听配置文件变化: %v", err)
		return
	}
	// 监听所在目录：编辑器和配置保存都会用新文件替换 config.toml
	if err := watcher.Add(filepath.Dir(configFile)); err != nil {
		log.Printf("[-] 无法监听配置文件变化: %v", err)
		watcher.Close()
		return
	}

	go func() {
		var reload *time.Timer
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(event.Name) != configFile || event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				// 将配置更改防抖至最多每秒一次
				if reload != nil {
					reload.Stop()
				}
				reload = time.AfterFunc(time.Second, func() {
					log.Println("[+] 配置文件已更新:", configFile)
					ReloadConfig()
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[-] 监听配置文件出错: %v", err)
			}
		}
	}()
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

	// 解析成功后再替换，失败时保留原配置
	cfg := &AppConfig{}
	if err := viper.Unmarshal(cfg); err != nil {
		log.Printf("[-] 重新解析配置失败: %v", err)
		return
	}
	_appConfig = cfg
}
