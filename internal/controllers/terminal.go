package controllers

import (
	"github.com/gin-gonic/gin"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

var TtydProcess *os.Process

//	func Terminal(ctx *gin.Context) {
//		ctx.HTML(http.StatusOK, "terminal.html", gin.H{})
//	}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func waitForServerReady(url string) {
	for {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func isCommandAvailable(name string) bool {
	cmd := exec.Command("which", name)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func installTtyd() error {
	cmd := exec.Command("sudo", "apt-get", "update")
	err := cmd.Run()
	if err != nil {
		return err
	}

	cmd = exec.Command("sudo", "apt-get", "install", "-y", "ttyd")
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func TerminalStart(ctx *gin.Context) {
	if !isCommandAvailable("ttyd") {
		log.Println("`ttyd` command is not available, trying to install it...")
		err := installTtyd()
		if err != nil {
			log.Println("Failed to install `ttyd`: ", err)
			return
		}
		log.Println("`ttyd` has been installed successfully.")
	}

	log.Println("执行命令")
	var shell string
	if isCommandAvailable("zsh") {
		shell = "zsh"
	} else {
		shell = "bash"
	}
	// 创建一个命令对象
	cmd := exec.Command("ttyd", "-t", "cursorStyle=bar", "-t", "lineHeight=1.2", "-t", "fontSize=14", shell)

	// 开启新的会话以创建守护进程
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// 启动命令
	err := cmd.Start()
	if err != nil {
		log.Println("Error starting command:", err)
		ctx.String(http.StatusInternalServerError, "Error starting command: %v", err)
		return
	}
	TtydProcess = cmd.Process
	log.Println("ttyd has been started with PID:", TtydProcess.Pid)
	waitForServerReady("http://" + getLocalIP() + ":7681/")
	log.Println("命令已完成")
	ctx.Redirect(http.StatusFound, "http://"+getLocalIP()+":7681/")
}

func TerminalStop(ctx *gin.Context) {
	if TtydProcess != nil {
		err := TtydProcess.Kill()
		if err != nil {
			log.Println("Error stopping command:", err)
			ctx.String(http.StatusInternalServerError, "Error stopping command: %v", err)
			return
		}
		TtydProcess = nil
		log.Println("ttyd has been stopped")
	} else {
		log.Println("ttyd is not running")
	}
	ctx.Redirect(http.StatusFound, "/")
}
