package test

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"testing"
)

func TestRedis(t *testing.T) {
	fmt.Println("golang连接redis")

	client := redis.NewClient(&redis.Options{})

	pong, err := client.Ping().Result()
	fmt.Println(pong, err)

	var name = make(gin.H)
	name["toto1"] = 1
	name["toto2"] = "hello"
	name["toto3"] = []string{}
	res, _ := json.Marshal(name)
	err = client.Set("npm:sites:golang", res, 0).Err()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("键golang设置成功")

	keys, err := client.Keys("nginx:*").Result()
	if err != nil {
		fmt.Println("获取key失败")
		return
	}
	for _, key := range keys {
		value, _ := client.Get(key).Result()
		var output gin.H
		json.Unmarshal([]byte(value), &output)
		fmt.Println(output["fileName"])
	}

}
