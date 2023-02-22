package config

import (
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"io/ioutil"
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
	viper.Unmarshal(&_appConfig)
	log.Println("[+] 配置文件载入成功")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if _appConfig.UUID == "" {
		log.Println("[-] 没有UUID，自动生成")
		uuid, _ := uuid.NewUUID()
		_appConfig.UUID = uuid.String()
		viper.Set("uuid", _appConfig.UUID)
		viper.WriteConfig()
		log.Println("[+] UUID 保存成功")
	}
	if _appConfig.IP == "" {
		ip := getIP()
		viper.Set("ip", ip)
		viper.WriteConfig()
		log.Println("[+] IP 保存成功")
	}
	//if _appConfig.ControlCenter == "" {
	//	log.Println("[-] 没有 ControlCenter")
	//	viper.Set("controlCenter", "https://misaka.qfdk.me/heartbeat")
	//	viper.WriteConfig()
	//	log.Println("[+] ControlCenter 保存成功")
	//}
}

func InitAppConfig() {
	log.Println("[+] 初始化配置文件 ...")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	pwd := tools.GetPWD()
	viper.AddConfigPath(pwd)
	if _, err := os.Stat(path.Join(pwd, "config.toml")); os.IsNotExist(err) {
		log.Println("[-] 未找到配置文件，生成并使用默认配置文件")
		//viper.Set("controlCenter", "https://misaka.qfdk.me/heartbeat")
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
		viper.SafeWriteConfig()
	}
	loadConfig()
	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		log.Println("[+] 配置文件更新了:", in.Name)
		viper.Unmarshal(&_appConfig)
	})
}

func getIP() string {
	log.Println("[-] 正在获取 IP ...")
	req, _ := http.NewRequest("GET", "https://api.ip.sb/ip", nil)
	req.Header.Set("User-Agent", "Mozilla")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	log.Printf("[+] IP 获取成功: %s\n", strings.TrimSpace(string(body)))
	return strings.TrimSpace(string(body))
}
