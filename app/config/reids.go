package config

import (
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"strings"
	"sync"
	"time"
)

var RedisClient *redis.Client
var once sync.Once

type RedisData struct {
	Content  string    `json:"content"`
	NotAfter time.Time `json:"notAfter"`
	Domains  string    `json:"domains"`
	FileName string    `json:"fileName"`
	Proxy    string    `json:"proxy"`
}

const RedisPrefix = "nginx:"

func InitRedis() {
	once.Do(func() {
		RedisClient = redis.NewClient(&redis.Options{})
	})
	pong, err := RedisClient.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Redis : " + pong)
}

func SaveSiteDataInRedis(fileName string, domains []string, content string, proxy string) {
	data := RedisData{
		FileName: fileName,
		Domains:  strings.Join(domains[:], ","),
		Content:  content,
		Proxy:    proxy,
	}
	res, _ := json.Marshal(data)
	RedisClient.Set(RedisPrefix+fileName, res, 0)
}
