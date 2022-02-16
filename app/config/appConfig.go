package config

import (
	"github.com/spf13/viper"
	"log"
	"os"
)

type AppConfig struct {
	VhostPath string
	Email     string
	SSLPath   string
	Username  string
	Password  string
	Id        string
	Url       string
	Redis     bool
}

var _appConfig *AppConfig = nil

func GetAppConfig() *AppConfig {
	if _appConfig == nil {
		InitAppConfig()
	}
	return _appConfig
}

func InitAppConfig() {
	log.Println("[+] 初始化配置文件 ...")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if _, err := os.Stat("./config.toml"); os.IsNotExist(err) {
		log.Println("[-] 未找到配置文件，使用默认配置文件")
		_appConfig = &AppConfig{
			VhostPath: "/etc/nginx/sites-enabled",
			SSLPath:   "/etc/nginx/ssl",
			Email:     "root@qfdk.me",
			Username:  "admin",
			Password:  "admin",
			Redis:     false,
		}
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		err := viper.ReadInConfig()
		if err != nil {
			log.Fatalf("[-] 读取配置文件失败: %v", err)
		}
		_appConfig = &AppConfig{}
		viper.Unmarshal(&_appConfig)
		log.Println("[+] 初始化配置文件载入成功")
	}
}
