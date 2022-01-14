package routers

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
	"time"
)

var upGrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

//webSocket请求ping 返回pong
func ws(c *gin.Context) {
	//升级get请求为webSocket协议
	ws, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	//读取ws中的数据
	mt, message, err := ws.ReadMessage()
	if err != nil {
		return
	}
	for {
		message, _ = json.Marshal(gin.H{"s": "s"})
		//写入ws数据
		err = ws.WriteMessage(mt, message)
		if err != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
}
