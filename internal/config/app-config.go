package config

import (
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
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
}

var _appConfig *AppConfig = nil

func GetAppConfig() *AppConfig {
	if _appConfig == nil {
		InitAppConfig()
	}
	return _appConfig
}

func loadConfig() {
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("[-] 读取配置文件失败: %v", err)
	}
	_appConfig = &AppConfig{}
	_ = viper.Unmarshal(&_appConfig)
	log.Println("[+] 配置文件载入成功")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if _appConfig.UUID == "" {
		log.Println("[-] 没有UUID，自动生成")
		uuid, _ := uuid.NewUUID()
		_appConfig.UUID = uuid.String()
		viper.Set("uuid", _appConfig.UUID)
		_ = viper.WriteConfig()
		log.Println("[+] UUID 保存成功")
	}
	if _appConfig.IP == "" {
		ip := getIP()
		viper.Set("ip", ip)
		_ = viper.WriteConfig()
		log.Println("[+] IP 保存成功")
	}
}

func InitAppConfig() {
	log.Println("[+] 初始化配置文件 ...")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	pwd := tools.GetPWD()
	viper.AddConfigPath(pwd)
	if _, err := os.Stat(path.Join(pwd, "config.toml")); os.IsNotExist(err) {
		log.Println("[-] 未找到配置文件，生成并使用默认配置文件")
		viper.Set("url", "http://localhost:7777")
		uuid, _ := uuid.NewUUID()
		viper.Set("uuid", uuid.String())
		viper.Set("token", "myToken")
		viper.Set("vhostPath", "/etc/nginx/sites-enabled")
		viper.Set("sslpath", "/etc/nginx/ssl")
		viper.Set("email", "hello@world.com")
		viper.Set("username", "admin")
		viper.Set("password", "admin")
		viper.Set("installPath", pwd)
		viper.Set("ip", getIP())
		_ = viper.SafeWriteConfig()
	}
	loadConfig()
	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		log.Println("[+] 配置文件更新了:", in.Name)
		_ = viper.Unmarshal(&_appConfig)
	})
}

func getIP() string {
	log.Println("[-] 正在获取 IP ...")
	req, err := http.NewRequest("GET", "https://api.ip.sb/ip", nil)
	if err != nil {
		log.Printf("请求创建失败: %v", err)
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("请求失败: %v", err)
		return ""
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Printf("请求返回非 200 状态码: %d", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取响应体失败: %v", err)
		return ""
	}

	ip := strings.TrimSpace(string(body))
	log.Printf("[+] IP 获取成功: %s\n", ip)
	return ip
}
