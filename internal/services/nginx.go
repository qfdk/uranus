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

	// nginxStartTimeout 是直接启动 nginx 后，等它的进程号出现在 PID 文件里的最长时间
	nginxStartTimeout = 5 * time.Second
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
	pidPath := config.NginxPidFile()
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

// systemdBooted reports whether this is a Linux host booted with systemd that
// has systemctl. Docker containers (including the provided image), OpenRC
// systems and macOS are not.
func systemdBooted() bool {
	if sysType != "linux" {
		return false
	}
	if info, err := os.Stat(systemdRunDir); err != nil || !info.IsDir() {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// nginxUnit returns the systemd unit nginx is installed as on this host.
// nginx built from source usually has none; it is then signalled directly.
func nginxUnit() (config.SystemdUnit, bool) {
	if !systemdBooted() {
		return config.SystemdUnit{}, false
	}
	return config.FindNginxUnit()
}

// managingUnit returns the systemd unit that is running nginx right now. A
// unit that exists but is inactive while nginx runs means nginx was started
// by hand: systemctl reload and stop would not reach it.
func managingUnit() (string, bool) {
	unit, ok := nginxUnit()
	if !ok || !unit.Active {
		return "", false
	}
	return unit.Name, true
}

// NginxManager describes how Uranus starts and stops nginx on this host.
func NginxManager() string {
	unit, ok := nginxUnit()
	switch {
	case !ok:
		return "直接控制 nginx 进程（nginx -s）"
	case unit.Active:
		return "systemd（" + unit.Name + "）"
	default:
		return "systemd（" + unit.Name + "，当前没有通过它运行）"
	}
}

// nginxCommand runs nginx with args for the main configuration file Uranus
// shows and edits, which is also the one the systemd unit starts nginx with.
func nginxCommand(args ...string) *exec.Cmd {
	if conf := config.ReadNginxCompileInfo().NginxConfPath; conf != "" {
		args = append([]string{"-c", conf}, args...)
	}
	return exec.Command(config.NginxBinary(), args...)
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

// run runs cmd and returns "OK" or why it failed: the output of nginx or
// systemctl and, for a systemd unit, the last lines of its journal.
func run(cmd *exec.Cmd, unit string) string {
	out, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) {
		// 命令本身成功退出了，只是它启动的后台进程还占着输出管道（见 startNginx）
		err = nil
	}
	invalidateNginxStatus()
	command := strings.Join(cmd.Args, " ")

	if err != nil {
		reason := strings.TrimSpace(string(out))
		if reason == "" {
			reason = err.Error()
		}
		if unit != "" {
			// systemctl 只说失败了，真正的原因在 nginx 的日志里
			if detail := journalTail(unit); detail != "" {
				reason += "\n" + detail
			}
		}
		log.Printf("[PID][%d]: [NGINX] %s failed: %s", syscall.Getpid(), command, reason)
		return reason
	}
	log.Printf("[PID][%d]: [NGINX] %s successful", syscall.Getpid(), command)
	return "OK"
}

// journalTail returns the last lines a unit logged, which explain a failed systemctl call.
func journalTail(unit string) string {
	out, err := exec.Command("journalctl", "-u", unit, "-n", "8", "--no-pager", "-o", "cat").Output()
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
	out, err := nginxCommand("-t").CombinedOutput()
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
	if readNginxPid() != "KO" {
		return "OK"
	}
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	return startNginx()
}

// startNginx starts a stopped nginx, through systemd when nginx is installed
// as a service so that systemd keeps managing it.
func startNginx() string {
	if unit, ok := nginxUnit(); ok {
		return run(exec.Command("systemctl", "start", unit.Name), unit.Name)
	}
	cmd := nginxCommand()
	// nginx 启动后转入后台。日志写到 stderr 时（官方 Docker 镜像把日志链接到 /dev/stdout 和
	// /dev/stderr，或者 error_log stderr），后台的 nginx 一直占着输出管道，要读到管道关闭就得
	// 等 nginx 退出。启动命令退出后最多再读一秒
	cmd.WaitDelay = time.Second
	if result := run(cmd, ""); result != "OK" {
		return result
	}
	// 启动命令在 nginx 转入后台后就成功返回，nginx 随后仍可能退出（比如写不了 PID 文件）
	if !waitNginxRunning(true, nginxStartTimeout) {
		return "已执行启动命令，但没有在 " + config.NginxPidFile() + " 找到运行中的 nginx 进程，原因请查看 nginx 的错误日志"
	}
	return "OK"
}

// ReloadNginx checks the configuration and reloads it. A stopped nginx has
// nothing to reload: the configuration applies when it starts.
func ReloadNginx() string {
	unit, managed := managingUnit()
	if !managed && NginxStatus() == "KO" {
		log.Println("[NGINX] Not running, no need to reload configuration!")
		return "OK"
	}
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	if managed {
		return run(exec.Command("systemctl", "reload", unit), unit)
	}
	return run(nginxCommand("-s", "reload"), "")
}

// StopNginx stops nginx gracefully, letting it finish the requests in flight.
func StopNginx() string {
	if unit, managed := managingUnit(); managed {
		return run(exec.Command("systemctl", "stop", unit), unit)
	}
	if readNginxPid() == "KO" {
		return "OK"
	}
	// quit 会等正在处理的请求结束再退出，nginx.service 停止时也是这样做的
	if result := run(nginxCommand("-s", "quit"), ""); result != "OK" {
		return result
	}
	if !waitNginxRunning(false, 15*time.Second) {
		return "已通知 Nginx 退出，但 15 秒后仍在运行（可能还有未处理完的连接）"
	}
	return "OK"
}

// RestartNginx checks the configuration, then stops nginx and starts it again.
func RestartNginx() string {
	if msg := TestNginxConfig(); msg != "" {
		return configCheckFailed(msg)
	}
	if unit, managed := managingUnit(); managed {
		return run(exec.Command("systemctl", "restart", unit), unit)
	}
	// 直接管理时要等旧进程完全退出再启动，否则端口还被占着
	if result := StopNginx(); result != "OK" {
		return result
	}
	return startNginx()
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

// IncludeSitesDir adds the sites directory to the http block of the main
// configuration. The default configuration of nginx built from source, of
// OpenResty and of Arch Linux includes no directory, so the sites saved by
// Uranus would not be loaded.
func IncludeSitesDir() string {
	sites := config.Sites()
	if !sites.Checked {
		return "无法读取或解析 nginx 主配置文件"
	}
	if sites.Included {
		return "OK"
	}
	confPath := config.ReadNginxCompileInfo().NginxConfPath
	src, err := os.ReadFile(confPath)
	if err != nil {
		return "读取 nginx 主配置文件失败：" + err.Error()
	}
	updated, err := config.AddSitesInclude(string(src), sites.Dir)
	if err != nil {
		return err.Error()
	}
	if err := os.MkdirAll(sites.Dir, 0o755); err != nil {
		return "创建网站目录失败：" + err.Error()
	}
	return WriteConfigChecked(confPath, []byte(updated))
}
