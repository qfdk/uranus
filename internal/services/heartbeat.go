package services

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	"uranus/internal/config"
)

func Heartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		go func() {
			hostname, _ := os.Hostname()
			data := gin.H{
				"buildTime":    config.BuildTime,
				"buildVersion": config.BuildVersion,
				"commitId":     config.CommitID,
				"goVersion":    runtime.Version(),
				"os":           runtime.GOOS,
				"url":          config.GetAppConfig().URL,
				"uuid":         config.GetAppConfig().UUID,
				"token":        config.GetAppConfig().Token,
				"ip":           config.GetAppConfig().IP,
				"hostname":     hostname,
				"activeTime":   time.Now().Format("2006-01-02 15:04:05"),
			}
			bytesData, err := json.Marshal(data)
			var response *http.Response
			if gin.Mode() == gin.ReleaseMode {
				response, err = http.Post(config.GetAppConfig().ControlCenter, "application/json", bytes.NewReader(bytesData))
			}
			if err != nil {
				log.Println(err)
			}
			defer func() {
				if response != nil {
					response.Body.Close()
				}
			}()
		}()
	}
}
