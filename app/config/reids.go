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
	fmt.Println("[+] 初始化 Redis ...")
	once.Do(func() {
		RedisClient = redis.NewClient(&redis.Options{})
	})
	pong, err := RedisClient.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Printf("[+] 初始化 Redis 成功 : %v\n", pong)
}

func CloseRedis() {
	RedisClient.Close()
}

func SaveSiteDataInRedis(fileName string, domains []string, content string, proxy string) {
	var redisData RedisData
	output, _ := RedisClient.Get(RedisPrefix + fileName).Result()
	json.Unmarshal([]byte(output), &redisData)
	redisData.FileName = fileName
	redisData.Domains = strings.Join(domains[:], ",")
	redisData.Content = content
	redisData.Proxy = proxy
	res, _ := json.Marshal(redisData)
	RedisClient.Set(RedisPrefix+fileName, res, 0)
}
