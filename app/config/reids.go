package config

import (
	"fmt"
	"github.com/go-redis/redis"
)

var client *redis.Client

func InitRedis() {
	client := redis.NewClient(&redis.Options{})
	pong, err := client.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("Redis 回复: " + pong)
}

func GetRedisClient() *redis.Client {
	return client
}
