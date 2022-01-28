package config

import (
	"github.com/spf13/viper"
	"log"
)

type AppConfig struct {
	VhostPath  string
	Email      string
	SSLPath    string
	Username   string
	Password   string
	MongodbUri string
}

var _appConfig *AppConfig = nil

func GetAppConfig() *AppConfig {
	if _appConfig == nil {
		readAppConfig()
	}
	return _appConfig
}

func readAppConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("读取配置文件失败: %v", err)
	}
	_appConfig = &AppConfig{}
	viper.Unmarshal(&_appConfig)
}
