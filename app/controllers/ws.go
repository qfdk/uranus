package controllers

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"net/http"
	"strconv"
	"time"
)

var upGrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Websocket 请求ping 返回pong
func Websocket(c *gin.Context) {
	//升级get请求为webSocket协议
	ws, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	response := make(gin.H)

	//读取ws中的数据
	mt, message, err := ws.ReadMessage()
	if err != nil {
		return
	}
	for {
		totalPercent, _ := cpu.Percent(1*time.Second, false)
		response["cpu"] = strconv.FormatFloat(totalPercent[0], 'f', 2, 64)

		memInfo, _ := mem.VirtualMemory()
		response["ram"] = strconv.FormatFloat(memInfo.UsedPercent, 'f', 2, 64)

		message, _ = json.Marshal(response)
		//写入ws数据
		err = ws.WriteMessage(mt, message)
		if err != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
}
