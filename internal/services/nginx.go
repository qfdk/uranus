package services

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"syscall"
	"time"
	"uranus/internal/config"
)

var (
	sysType           = runtime.GOOS
	nginxStatusCache  string
	nginxStatusMutex  sync.RWMutex
	nginxStatusExpiry time.Time

	// Cache for nginx configuration path
	nginxConfPathCache string
	nginxConfPathMutex sync.RWMutex
)

// NginxStatus returns the status of Nginx with caching for better performance
func NginxStatus() string {
	// Check if cache is valid (read lock)
	nginxStatusMutex.RLock()
	if time.Now().Before(nginxStatusExpiry) && nginxStatusCache != "" {
		status := nginxStatusCache
		nginxStatusMutex.RUnlock()
		log.Printf("[NGINX] Using cached status: %s", status)
		return status
	}
	nginxStatusMutex.RUnlock()

	// Cache is invalid, get new status (write lock)
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	log.Printf("[NGINX] Checking PID file at: %s", pidPath)

	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result string

	if err != nil {
		log.Printf("[NGINX] Failed to read PID file: %v", err)
		result = "KO"
	} else {
		result = string(out)
		log.Printf("[NGINX] PID file content: %s", result)
	}

	// Update cache
	nginxStatusMutex.Lock()
	nginxStatusCache = result
	nginxStatusExpiry = time.Now().Add(5 * time.Second) // Cache for 5 seconds
	nginxStatusMutex.Unlock()

	return result
}

func StartNginx() string {
	var out []byte
	var err error

	// Command varies by OS
	if sysType == "darwin" {
		out, err = exec.Command("nginx").CombinedOutput()
	} else {
		out, err = exec.Command("systemctl", "start", "nginx").CombinedOutput()
	}

	var result = "OK"
	if err != nil {
		result = err.Error()
		log.Printf("[PID][%d]: [NGINX] Start failed, %s", syscall.Getpid(), out)
	} else {
		log.Printf("[PID][%d]: [NGINX] Start successful", syscall.Getpid())
	}

	// Invalidate status cache to ensure fresh status after operation
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Now()
	nginxStatusMutex.Unlock()

	return result
}

func ReloadNginx() string {
	log.Println("[NGINX] Reloading configuration")
	var result = "OK"

	// 检查Nginx状态
	status := NginxStatus()
	log.Printf("[NGINX] Current status: %s", status)

	if status != "KO" {
		var out []byte
		var err error

		if sysType == "darwin" {
			log.Printf("[NGINX] 在macOS上执行: nginx -s reload")
			out, err = exec.Command("nginx", "-s", "reload").CombinedOutput()
		} else {
			log.Printf("[NGINX] 在Linux上执行: systemctl reload nginx")
			out, err = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
		}

		if err != nil {
			log.Printf("[NGINX] Error reloading configuration: %v, output: %s", err, string(out))
			result = string(out)
		} else {
			log.Printf("[NGINX] 重载成功，输出: %s", string(out))
		}
	} else {
		log.Println("[NGINX] Not running, no need to reload configuration!")
	}

	// Invalidate status cache
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Now()
	nginxStatusMutex.Unlock()

	return result
}

func StopNginx() string {
	log.Printf("[PID][%d]: [NGINX] Stopping", syscall.Getpid())
	var err error

	if sysType == "darwin" {
		_, err = exec.Command("nginx", "-s", "stop").CombinedOutput()
	} else {
		_, err = exec.Command("systemctl", "stop", "nginx").CombinedOutput()
	}

	var result = "OK"
	if err != nil {
		log.Printf("[PID][%d]: [NGINX] Error stopping", syscall.Getpid())
		log.Println(err)
		result = "KO"
	}

	// Invalidate status cache
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Now()
	nginxStatusMutex.Unlock()

	return result
}

// Get Nginx configuration path with caching
func getNginxConfPath() string {
	// Check if cache is valid (read lock)
	nginxConfPathMutex.RLock()
	if nginxConfPathCache != "" {
		path := nginxConfPathCache
		nginxConfPathMutex.RUnlock()
		return path
	}
	nginxConfPathMutex.RUnlock()

	// Cache miss, compute the path (write lock)
	configPath := config.ReadNginxCompileInfo().NginxConfPath
	regex, _ := regexp.Compile("(.*)/(.*.conf)")
	confPath := regex.FindStringSubmatch(configPath)[1]

	// Update cache
	nginxConfPathMutex.Lock()
	nginxConfPathCache = confPath
	nginxConfPathMutex.Unlock()

	return confPath
}

func SaveNginxConf(content string) string {
	path := filepath.Join(getNginxConfPath(), "nginx.conf")
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		log.Printf("[NGINX] Error saving configuration: %v", err)
		return "Error saving configuration: " + err.Error()
	}
	log.Println("[NGINX] Configuration saved successfully")
	return ReloadNginx()
}
