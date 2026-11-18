package services

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

	// systemdRunDir 存在说明本机由 systemd 启动（同 sd_booted(3) 的判断方式）
	systemdRunDir = "/run/systemd/system"
)

// NginxStatus returns the PID of the running nginx master, or "KO", with caching for better performance
func NginxStatus() string {
	// Check if cache is valid (read lock)
	nginxStatusMutex.RLock()
	if time.Now().Before(nginxStatusExpiry) && nginxStatusCache != "" {
		status := nginxStatusCache
		nginxStatusMutex.RUnlock()
		return status
	}
	nginxStatusMutex.RUnlock()

	result := readNginxPid()

	// Update cache
	nginxStatusMutex.Lock()
	nginxStatusCache = result
	nginxStatusExpiry = time.Now().Add(5 * time.Second) // Cache for 5 seconds
	nginxStatusMutex.Unlock()

	return result
}

// readNginxPid reads nginx's PID file and makes sure that process still exists:
// a crashed or killed nginx leaves its PID file behind.
func readNginxPid() string {
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	if pidPath == "" {
		return "KO"
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return "KO"
	}
	pidText := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return "KO"
	}
	// 信号 0 只检查进程是否存在；EPERM 表示进程存在但属于其他用户
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return "KO"
	}
	return pidText
}

// useSystemd reports whether nginx is managed with systemctl. That is only the
// case on Linux hosts booted with systemd; Docker containers (including the
// provided image), OpenRC systems and macOS signal nginx directly.
func useSystemd() bool {
	if sysType != "linux" {
		return false
	}
	if info, err := os.Stat(systemdRunDir); err != nil || !info.IsDir() {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// nginxCommand builds the command that starts, reloads or stops nginx.
func nginxCommand(action string) *exec.Cmd {
	if useSystemd() {
		return exec.Command("systemctl", action, "nginx")
	}
	nginx := config.NginxBinary()
	if action == "start" {
		return exec.Command(nginx)
	}
	return exec.Command(nginx, "-s", action)
}

// runNginxAction runs action and returns "OK" or why it failed.
func runNginxAction(action string) string {
	out, err := nginxCommand(action).CombinedOutput()

	// Invalidate status cache to ensure fresh status after operation
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Time{}
	nginxStatusMutex.Unlock()

	if err != nil {
		reason := strings.TrimSpace(string(out))
		if reason == "" {
			reason = err.Error()
		}
		log.Printf("[PID][%d]: [NGINX] %s failed: %s", syscall.Getpid(), action, reason)
		return reason
	}
	log.Printf("[PID][%d]: [NGINX] %s successful", syscall.Getpid(), action)
	return "OK"
}

func StartNginx() string {
	return runNginxAction("start")
}

func ReloadNginx() string {
	if NginxStatus() == "KO" {
		log.Println("[NGINX] Not running, no need to reload configuration!")
		return "OK"
	}
	return runNginxAction("reload")
}

func StopNginx() string {
	return runNginxAction("stop")
}

// SaveNginxConf writes the main nginx configuration file, the same file that
// is shown in the editor, and reloads nginx.
func SaveNginxConf(content string) string {
	confPath := config.ReadNginxCompileInfo().NginxConfPath
	if confPath == "" {
		return "未找到 nginx 主配置文件，请确认 nginx 已安装"
	}
	if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
		log.Printf("[NGINX] Error saving configuration: %v", err)
		return "Error saving configuration: " + err.Error()
	}
	log.Println("[NGINX] Configuration saved successfully")
	return ReloadNginx()
}
