package config

import (
	"github.com/spf13/viper"
	"log"
)

type AppConfig struct {
	VhostPath string
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
		log.Fatalf("read config failed: %v", err)
	}
	_appConfig = &AppConfig{}
	viper.Unmarshal(&_appConfig)
}
