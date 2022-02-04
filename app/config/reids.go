package config

import (
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"strings"
	"sync"
	"time"
)

var client *redis.Client
var once sync.Once

type RedisData struct {
	Content  string `json:"content"`
	Expired  int64  `json:"expired"`
	Domains  string `json:"domains"`
	FileName string `json:"fileName"`
	Proxy    string `json:"proxy"`
}

const redisPrefix = "nginx:"

func InitRedis() {
	once.Do(func() {
		client = redis.NewClient(&redis.Options{})
	})
	pong, err := client.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Redis : " + pong)
}

//func getRedisClient() *redis.Client {
//	return client
//}

func RedisGet(key string) string {
	redisData, _ := client.Get(redisPrefix + key).Result()
	return redisData
}

func RedisDel(key string) {
	client.Del(redisPrefix + key)
}

func RedisSet(key string, content []byte) {
	client.Set(redisPrefix+key, content, 0)
}

func RedisSetWithTTL(key string, content string, expiration time.Duration) {
	client.Set(redisPrefix+key, content, expiration)
}

func RedisKeys() []string {
	keys, _ := client.Keys(redisPrefix + "*").Result()
	return keys
}

func SaveSiteDataInRedis(fileName string, domains []string, content string, proxy string) {
	dd, _ := time.ParseDuration("24h")
	data := RedisData{
		FileName: fileName,
		Domains:  strings.Join(domains[:], ","),
		Content:  content,
		Expired:  time.Now().Add(dd * 80).Unix(),
		Proxy:    proxy,
	}
	res, _ := json.Marshal(data)
	RedisSet(fileName, res)
}
