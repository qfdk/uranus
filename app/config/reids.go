package config

import (
	"fmt"
	"github.com/go-redis/redis"
	"sync"
	"time"
)

var client *redis.Client
var once sync.Once

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
