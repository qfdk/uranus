package config

import (
	"fmt"
	"github.com/go-redis/redis"
	"sync"
)

var client *redis.Client
var once sync.Once

func InitRedis() {
	once.Do(func() {
		client = redis.NewClient(&redis.Options{})
	})
	pong, err := client.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Redis 连接回复: " + pong)
}

func GetRedisClient() *redis.Client {
	return client
}
