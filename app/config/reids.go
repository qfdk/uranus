package config

import (
	"fmt"
	"github.com/go-redis/redis"
	"sync"
	"time"
)

var client *redis.Client
var once sync.Once

const RedisPrefix = "nginx:"

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

func getRedisClient() *redis.Client {
	return client
}

func RedisGet(key string) string {
	redisData, _ := client.Get(RedisPrefix + key).Result()
	return redisData
}

func RedisDel(key string) {
	client.Del(RedisPrefix + key)
}
func RedisSet(key string, content []byte) {
	client.Set(RedisPrefix+key, content, 0)
}

func RedisSetWithTTL(key string, content string, expiration time.Duration) {
	client.Set(RedisPrefix+key, content, expiration)
}
