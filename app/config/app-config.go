package config

import (
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"log"
	"os"
	"path"
	"uranus/app/tools"
)

type AppConfig struct {
	VhostPath   string
	Email       string
	SSLPath     string
	Username    string
	Password    string
	Uid         string
	Url         string
	Token       string
	InstallPath string
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
}

func InitAppConfig() {
	log.Println("[+] 初始化配置文件 ...")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	pwd := tools.GetPWD()
	viper.AddConfigPath(pwd)
	if _, err := os.Stat(path.Join(pwd, "config.toml")); os.IsNotExist(err) {
		log.Println("[-] 未找到配置文件，生成并使用默认配置文件")
		viper.Set("VhostPath", "/etc/nginx/sites-enabled")
		viper.Set("SSLPath", "/etc/nginx/ssl")
		viper.Set("Email", "hello@world.com")
		viper.Set("Username", "admin")
		viper.Set("Password", "admin")
		viper.Set("Url", "https://misaka.qfdk.me")
		viper.Set("Uid", "# Anonymous")
		viper.Set("Token", "myToken")
		viper.Set("InstallPath", pwd)
		viper.SafeWriteConfig()
	}
	loadConfig()
	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		log.Println("[+] 配置文件更新了:", in.Name)
		loadConfig()
	})
}
