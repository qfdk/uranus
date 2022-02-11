package config

import (
	"fmt"
	"github.com/spf13/viper"
	"os"
)

type AppConfig struct {
	VhostPath string
	Email     string
	SSLPath   string
	Username  string
	Password  string
}

var _appConfig *AppConfig = nil

func GetAppConfig() *AppConfig {
	if _appConfig == nil {
		InitAppConfig()
	}
	return _appConfig
}

func InitAppConfig() {
	fmt.Println("[+] 初始化配置文件 ...")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if _, err := os.Stat("./config.toml"); os.IsNotExist(err) {
		fmt.Println("[-] 未找到配置文件，使用默认配置文件")
		_appConfig = &AppConfig{
			VhostPath: "/etc/nginx/sites-enabled",
			SSLPath:   "/etc/nginx/ssl",
			Email:     "root@qfdk.me",
			Username:  "admin",
			Password:  "admin",
		}
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
		err := viper.ReadInConfig()
		if err != nil {
			fmt.Errorf("[-] 读取配置文件失败: %v", err)
		}
		_appConfig = &AppConfig{}
		viper.Unmarshal(&_appConfig)
		fmt.Println("[+] 初始化配置文件载入成功")
	}
}
