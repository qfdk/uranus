package controllers

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var TtydProcess *os.Process

func getIP() string {
	// 先尝试获取公网IP
	publicIP, err := getPublicIP()
	if err == nil && publicIP != "" {
		return strings.TrimSpace(publicIP)
	}

	// 如果无法获取公网IP，则获取本地IP
	localIP := getLocalIP()
	if localIP != "" {
		return strings.TrimSpace(localIP)
	}

	// 如果无法获取任何IP，则返回空字符串
	return ""
}

func getPublicIP() (string, error) {
	resp, err := http.Get("http://ip.tar.tn")
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

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

func stopTtyd() error {
	// Attempt to kill the process directly if we have a handle
	if TtydProcess != nil {
		err := TtydProcess.Kill()
		if err == nil {
			log.Println("Killed ttyd process directly")
			return nil
		}
		log.Println("Error killing ttyd process directly:", err)
	}

	// Fallback to systemctl if we're on Linux
	if runtime.GOOS == "linux" {
		cmd := exec.Command("sudo", "systemctl", "stop", "ttyd")
		err := cmd.Run()
		if err != nil {
			log.Println("Error stopping ttyd service:", err)
			return err
		}
		log.Println("Stopped ttyd service using systemctl")
		return nil
	}

	// On macOS, try to find and kill the process
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pkill", "-f", "ttyd")
		_ = cmd.Run() // Ignore errors as process might not exist
		log.Println("Attempted to kill ttyd process with pkill")
	}

	return nil
}

func installTtyd() error {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// Check if Homebrew is installed
		_, err := exec.LookPath("brew")
		if err != nil {
			return fmt.Errorf("Homebrew is not installed. Please install Homebrew first")
		}

		// Install ttyd using Homebrew
		cmd = exec.Command("brew", "install", "ttyd")
	} else {
		// Default Linux installation
		cmd = exec.Command("sudo", "apt-get", "update")
		err := cmd.Run()
		if err != nil {
			return err
		}

		cmd = exec.Command("sudo", "apt-get", "install", "-y", "ttyd")
	}

	err := cmd.Run()
	if err != nil {
		return err
	}

	time.Sleep(2 * time.Second)
	return stopTtyd()
}

func TerminalStart(ctx *gin.Context) {
	// 检查TtydProcess是否存在
	if TtydProcess != nil {
		log.Println("ttyd process is already running.")
		ctx.Redirect(http.StatusFound, "http://"+getIP()+":7681/")
		return
	}

	if err := ensureTtydInstalled(); err != nil {
		ctx.String(http.StatusInternalServerError, "Error preparing ttyd: %v", err)
		return
	}

	if err := configureTtydAccess(); err != nil {
		log.Printf("Warning: Could not configure network access: %v", err)
		// Continue anyway, might work without explicit firewall rules
	}

	shell := findAvailableShell()

	// 创建一个命令对象
	cmd := exec.Command("ttyd", "-t", "cursorStyle=bar", "-t", "lineHeight=1.2", "-t", "fontSize=14", "-O", "login", shell)

	// 开启新的会话以创建守护进程
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		log.Println("Error starting command:", err)
		ctx.String(http.StatusInternalServerError, "Error starting command: %v", err)
		return
	}

	TtydProcess = cmd.Process
	log.Println("ttyd has been started with PID:", TtydProcess.Pid)
	waitForServerReady("http://" + getIP() + ":7681/")
	ctx.Redirect(http.StatusFound, "http://"+getIP()+":7681/")
}

func ensureTtydInstalled() error {
	if isCommandAvailable("ttyd") {
		return nil
	}

	log.Println("`ttyd` command is not available, trying to install it...")
	err := installTtyd()
	if err != nil {
		log.Println("Failed to install `ttyd`: ", err)
		return err
	}
	log.Println("`ttyd` has been installed successfully.")
	return nil
}

func configureTtydAccess() error {
	log.Println("Configuring network access for ttyd on port 7681")

	// Get the current IP address
	IP := getIP()
	if IP == "" {
		return fmt.Errorf("failed to determine IP address")
	}

	if runtime.GOOS == "darwin" {
		// macOS uses pf for firewall, but we'll skip explicit configuration
		// as it's often not enabled by default and requires additional setup
		log.Println("Running on macOS - skipping firewall configuration")
		return nil
	} else if runtime.GOOS == "linux" {
		// For Linux systems, try to use iptables
		return configureIptables(IP)
	}

	// For other OSes, just log and continue
	log.Printf("No firewall configuration implemented for %s", runtime.GOOS)
	return nil
}

func configureIptables(ip string) error {
	// Check if iptables is available
	if !isCommandAvailable("iptables") {
		return fmt.Errorf("iptables is not available")
	}

	// Check if rule already exists
	output, err := exec.Command("sh", "-c", "sudo iptables -L | grep '7681'").Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ExitCode() == 1 {
				// grep没有找到匹配的行，继续执行后面的代码
				log.Println("`grep`没有找到匹配的行，继续执行后面的代码")
			} else {
				log.Println("Error reading iptables rules:", err)
				return err
			}
		} else {
			log.Println("Error reading iptables rules:", err)
			return err
		}
	}

	// 如果已经有相应的规则，不再添加
	if strings.Contains(string(output), "7681") {
		log.Println("iptables rule for port 7681 already exists")
		return nil
	}

	log.Println("执行 iptables 添加规则命令 : ", ip)
	iptablesCmd := exec.Command("sudo", "iptables", "-I", "INPUT", "-s", ip, "-p", "tcp", "--dport", "7681", "-j", "ACCEPT")
	err = iptablesCmd.Run()
	if err != nil {
		log.Println("Error adding iptables rule:", err)
		return err
	}

	return nil
}

func findAvailableShell() string {
	if isCommandAvailable("zsh") {
		return "zsh"
	}

	return "bash"
}

func TerminalStop(ctx *gin.Context) {
	if TtydProcess != nil {
		log.Println("Stopping ttyd process...")
		_ = stopTtyd()

		if runtime.GOOS == "linux" {
			// Remove iptables rules on Linux
			removeIptablesRules()
		}

		TtydProcess = nil
		log.Println("ttyd has been stopped")
	} else {
		log.Println("ttyd is not running")
	}

	ctx.Redirect(http.StatusFound, "/")
}

func removeIptablesRules() {
	// Only attempt to remove iptables rules on Linux
	if runtime.GOOS != "linux" {
		return
	}

	if !isCommandAvailable("iptables") {
		log.Println("iptables is not available, skipping rule removal")
		return
	}

	log.Println("正在移除 iptables 规则")
	rules, err := exec.Command("sh", "-c", "sudo iptables -L --line-numbers | grep '7681'").Output()
	if err != nil {
		log.Println("Error listing iptables rules:", err)
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(rules))
	for scanner.Scan() {
		line := scanner.Text()
		lineSplit := strings.Split(line, " ")
		if len(lineSplit) > 0 {
			lineNum := lineSplit[0]
			deleteCmd := exec.Command("sudo", "iptables", "-D", "INPUT", lineNum)
			if err := deleteCmd.Run(); err != nil {
				log.Println("Error removing iptables rule:", err)
			} else {
				log.Printf("Removed iptables rule %s", lineNum)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Println("Error reading iptables rules:", err)
	}
}
