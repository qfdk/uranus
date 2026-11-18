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

// nginxCommand builds the command that starts, reloads, stops or restarts nginx.
// Restart only exists under systemd; RestartNginx handles the direct case.
func nginxCommand(action string) *exec.Cmd {
	if useSystemd() {
		return exec.Command("systemctl", action, "nginx")
	}
	nginx := config.NginxBinary()
	switch action {
	case "start":
		return exec.Command(nginx)
	case "stop":
		// quit 会等正在处理的请求结束再退出，nginx.service 停止时也是这样做的
		return exec.Command(nginx, "-s", "quit")
	default:
		return exec.Command(nginx, "-s", action)
	}
}

func invalidateNginxStatus() {
	nginxStatusMutex.Lock()
	nginxStatusExpiry = time.Time{}
	nginxStatusMutex.Unlock()
}

// waitNginxRunning waits until nginx is (or is no longer) running. Signalling
// nginx directly returns as soon as the signal is sent, unlike systemctl.
func waitNginxRunning(running bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if (readNginxPid() != "KO") == running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// runNginxAction runs action and returns "OK" or why it failed.
func runNginxAction(action string) string {
	out, err := nginxCommand(action).CombinedOutput()
	defer invalidateNginxStatus()

	if err != nil {
		reason := strings.TrimSpace(string(out))
		if reason == "" {
			reason = err.Error()
		}
		if useSystemd() {
			// systemctl 只说失败了，真正的原因在 nginx 的日志里
			if detail := journalTail(); detail != "" {
				reason += "\n" + detail
			}
		}
		log.Printf("[PID][%d]: [NGINX] %s failed: %s", syscall.Getpid(), action, reason)
		return reason
	}

	if !useSystemd() {
		switch action {
		case "start":
			waitNginxRunning(true, 5*time.Second)
		case "stop":
			if !waitNginxRunning(false, 15*time.Second) {
				return "已通知 Nginx 退出，但 15 秒后仍在运行（可能还有未处理完的连接）"
			}
		}
	}
	log.Printf("[PID][%d]: [NGINX] %s successful", syscall.Getpid(), action)
	return "OK"
}

// journalTail returns the last lines nginx.service logged, which explain a failed systemctl call.
func journalTail() string {
	out, err := exec.Command("journalctl", "-u", "nginx", "-n", "8", "--no-pager", "-o", "cat").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// nginxInstalled reports whether nginx was found on this machine.
func nginxInstalled() bool {
	return config.ReadNginxCompileInfo().NginxConfPath != ""
}

// TestNginxConfig runs `nginx -t` and returns "" when the configuration is
// valid, otherwise nginx's explanation (which file and line is wrong).
func TestNginxConfig() string {
	out, err := exec.Command(config.NginxBinary(), "-t").CombinedOutput()
	if err == nil {
		return ""
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return msg
	}
	return err.Error()
}

func configCheckFailed(detail string) string {
	return "配置检查未通过：\n" + detail
}

// StartNginx checks the configuration and starts nginx.
func StartNginx() string {
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	return runNginxAction("start")
}

// ReloadNginx checks the configuration and reloads it. A stopped nginx has
// nothing to reload: the configuration applies when it starts.
func ReloadNginx() string {
	if NginxStatus() == "KO" {
		log.Println("[NGINX] Not running, no need to reload configuration!")
		return "OK"
	}
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	return runNginxAction("reload")
}

// StopNginx stops nginx gracefully, letting it finish the requests in flight.
func StopNginx() string {
	return runNginxAction("stop")
}

// RestartNginx checks the configuration, then stops nginx and starts it again.
func RestartNginx() string {
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	if useSystemd() {
		return runNginxAction("restart")
	}
	// 直接管理时要等旧进程完全退出再启动，否则端口还被占着
	if readNginxPid() != "KO" {
		if result := runNginxAction("stop"); result != "OK" {
			return result
		}
	}
	return runNginxAction("start")
}

// WriteConfigChecked writes an nginx configuration file and checks the whole
// configuration with `nginx -t`. When the check fails the previous content is
// restored (or the new file removed) so a broken file never stays on disk;
// otherwise nginx is reloaded.
func WriteConfigChecked(path string, content []byte) string {
	previous, readErr := os.ReadFile(path)
	existed := readErr == nil

	if err := os.WriteFile(path, content, 0644); err != nil {
		log.Printf("[NGINX] Error saving configuration: %v", err)
		return "写入配置文件失败：" + err.Error()
	}

	if nginxInstalled() {
		if msg := TestNginxConfig(); msg != "" {
			if existed {
				_ = os.WriteFile(path, previous, 0644)
			} else {
				_ = os.Remove(path)
			}
			log.Printf("[NGINX] Configuration check failed, %s restored", path)
			return "配置检查未通过，这次修改没有保存：\n" + msg
		}
	}

	log.Printf("[NGINX] Configuration saved: %s", path)
	return ReloadNginx()
}

// SaveNginxConf writes the main nginx configuration file, the same file that
// is shown in the editor, and reloads nginx.
func SaveNginxConf(content string) string {
	confPath := config.ReadNginxCompileInfo().NginxConfPath
	if confPath == "" {
		return "未找到 nginx 主配置文件，请确认 nginx 已安装"
	}
	return WriteConfigChecked(confPath, []byte(content))
}
