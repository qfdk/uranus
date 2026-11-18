package config

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

type NginxCompileInfo struct {
	Version         string
	CompilerVersion string
	SSLVersion      string
	TLSSupport      string
	NginxExec       string
	NginxConfPath   string
	NginxPidPath    string
	Params          []string
}

var (
	_nci            *NginxCompileInfo
	nciInstalled    bool
	nciMutex        sync.Mutex
	autoInstallOnce sync.Once

	// nginxSearchPaths 是 PATH 中找不到 nginx 时依次尝试的位置。
	// 例如 macOS 上由 launchd 启动时 PATH 不包含 Homebrew 目录。
	nginxSearchPaths = []string{
		"/opt/homebrew/bin/nginx", // Homebrew (Apple Silicon)
		"/usr/local/bin/nginx",    // Homebrew (Intel)
		"/usr/sbin/nginx",
		"/usr/local/sbin/nginx",
		"/usr/local/nginx/sbin/nginx", // 源码编译的默认位置
		"/usr/local/openresty/nginx/sbin/nginx",
	}
)

// NginxBinary returns the nginx executable: the one on PATH, otherwise the
// first one found in the usual install locations.
func NginxBinary() string {
	if p, err := exec.LookPath("nginx"); err == nil {
		return p
	}
	for _, p := range nginxSearchPaths {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return "nginx"
}

// ReadNginxCompileInfo returns the parsed `nginx -V` output. Once nginx is found
// the result is cached; while it is missing every call looks again, so nginx
// installed later (e.g. from the web terminal) is picked up without a restart.
func ReadNginxCompileInfo() *NginxCompileInfo {
	nciMutex.Lock()
	defer nciMutex.Unlock()

	if !nciInstalled {
		_nci, nciInstalled = detectNginx()
	}
	return _nci
}

func detectNginx() (*NginxCompileInfo, bool) {
	out, err := exec.Command(NginxBinary(), "-V").CombinedOutput()
	if err != nil {
		installed := false
		// 只在第一次发现缺少 nginx 时提示安装或尝试自动安装
		autoInstallOnce.Do(func() { installed = installNginx(err) })
		if !installed {
			return notInstalledInfo(), false
		}
		if out, err = exec.Command(NginxBinary(), "-V").CombinedOutput(); err != nil {
			return notInstalledInfo(), false
		}
		log.Printf("[+] Nginx installation verified, continuing with initialization...")
	}
	return parseNginxInfo(string(out)), true
}

func notInstalledInfo() *NginxCompileInfo {
	return &NginxCompileInfo{
		Version:         "Not installed - Please install nginx",
		CompilerVersion: "N/A",
		SSLVersion:      "N/A",
		TLSSupport:      "N/A",
		Params:          []string{},
	}
}

// installNginx prints install instructions and, when running as root on a
// supported Linux distribution, installs nginx with the package manager.
func installNginx(cause error) bool {
	log.Printf("[-] Nginx doesn't seem to be installed: %v", cause)

	if runtime.GOOS == "darwin" {
		log.Printf("[!] Please install nginx with Homebrew: brew install nginx")
		return false
	}

	// 检查是否以root权限运行
	if os.Geteuid() != 0 {
		log.Printf("[!] Please install nginx manually or run as root for auto-installation:")
		log.Printf("    Ubuntu/Debian: sudo apt update && sudo apt install nginx")
		log.Printf("    CentOS/RHEL:   sudo yum install nginx")
		log.Printf("    Rocky/Alma:    sudo dnf install nginx")
		return false
	}

	log.Printf("[!] Attempting to auto-install nginx...")
	if autoInstallNginx() {
		log.Printf("[+] Nginx installed successfully! Re-initializing...")
		return true
	}
	log.Printf("[-] Auto-installation failed. Please install nginx manually")
	return false
}

// parseNginxInfo parses `nginx -V` output
func parseNginxInfo(output string) *NginxCompileInfo {
	lines := strings.Split(output, "\n")
	nci := &NginxCompileInfo{}

	// Parse version
	versionRegex := regexp.MustCompile(`nginx version: (.+)`)
	if matches := versionRegex.FindStringSubmatch(lines[0]); len(matches) > 1 {
		nci.Version = matches[1]
	}

	// Parse compiler and SSL info based on format
	if len(lines) > 1 && strings.Contains(lines[1], "built with") {
		nci.CompilerVersion = "Non-compiled version"

		sslRegex := regexp.MustCompile(`built with (.+)`)
		if matches := sslRegex.FindStringSubmatch(lines[1]); len(matches) > 1 {
			nci.SSLVersion = matches[1]
		}

		if len(lines) > 2 {
			nci.TLSSupport = lines[2]
		}
	} else if len(lines) > 1 {
		compilerRegex := regexp.MustCompile(`built by (.+)`)
		if matches := compilerRegex.FindStringSubmatch(lines[1]); len(matches) > 1 {
			nci.CompilerVersion = matches[1]
		}

		if len(lines) > 2 {
			sslRegex := regexp.MustCompile(`built with (.+)`)
			if matches := sslRegex.FindStringSubmatch(lines[2]); len(matches) > 1 {
				nci.SSLVersion = matches[1]
			}
		}

		if len(lines) > 3 {
			nci.TLSSupport = lines[3]
		}
	}

	// Parse configure arguments in a single pass
	configRegex := regexp.MustCompile(`configure arguments: (.+)`)
	var configArgs string
	if matches := configRegex.FindStringSubmatch(output); len(matches) > 1 {
		configArgs = matches[1]
	}

	prefix := ""
	if configArgs != "" {
		// 按 -- 拆分配置参数，去掉拆分产生的空项
		for _, param := range strings.Split(configArgs, "--") {
			if param = strings.TrimSpace(param); param != "" {
				nci.Params = append(nci.Params, param)
			}
		}

		// Extract paths from parameters
		for _, param := range nci.Params {
			name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			switch name {
			case "prefix":
				prefix = value
			case "sbin-path":
				nci.NginxExec = value
			case "conf-path":
				nci.NginxConfPath = value
			case "pid-path":
				nci.NginxPidPath = value
			}
		}
	}

	// 源码编译或 OpenResty 等未显式指定路径时，nginx 使用 prefix 下的默认位置
	if prefix == "" {
		prefix = "/usr/local/nginx"
	}
	nci.NginxExec = underPrefix(prefix, nci.NginxExec, "sbin/nginx")
	nci.NginxConfPath = underPrefix(prefix, nci.NginxConfPath, "conf/nginx.conf")
	nci.NginxPidPath = underPrefix(prefix, nci.NginxPidPath, "logs/nginx.pid")

	return nci
}

// underPrefix resolves a configure path the way nginx does: relative paths
// are relative to the prefix, and a missing one falls back to the default.
func underPrefix(prefix, value, fallback string) string {
	if value == "" {
		value = fallback
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(prefix, value)
}

// autoInstallNginx attempts to automatically install nginx based on the detected Linux distribution
func autoInstallNginx() bool {
	log.Printf("[*] Detecting Linux distribution...")

	// 检测Ubuntu/Debian
	if _, err := os.Stat("/etc/debian_version"); err == nil {
		log.Printf("[*] Detected Debian/Ubuntu system")
		return runCommand("apt", "update") && runCommand("apt", "install", "-y", "nginx")
	}

	// 检测CentOS/RHEL (有/etc/redhat-release)
	if _, err := os.Stat("/etc/redhat-release"); err == nil {
		// 先尝试dnf (较新的系统)
		if _, err := exec.LookPath("dnf"); err == nil {
			log.Printf("[*] Detected RHEL/CentOS/Rocky/Alma with dnf")
			return runCommand("dnf", "install", "-y", "nginx")
		}
		// 回退到yum (较老的系统)
		if _, err := exec.LookPath("yum"); err == nil {
			log.Printf("[*] Detected RHEL/CentOS with yum")
			return runCommand("yum", "install", "-y", "nginx")
		}
	}

	// 检测Alpine
	if _, err := os.Stat("/etc/alpine-release"); err == nil {
		log.Printf("[*] Detected Alpine Linux")
		return runCommand("apk", "update") && runCommand("apk", "add", "nginx")
	}

	// 检测Arch Linux
	if _, err := os.Stat("/etc/arch-release"); err == nil {
		log.Printf("[*] Detected Arch Linux")
		return runCommand("pacman", "-Sy", "--noconfirm", "nginx")
	}

	log.Printf("[-] Unsupported Linux distribution for auto-installation")
	return false
}

// runCommand executes a command with arguments and returns success status
func runCommand(command string, args ...string) bool {
	log.Printf("[*] Running: %s %s", command, strings.Join(args, " "))
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[-] Command failed: %v", err)
		if len(output) > 0 {
			log.Printf("[-] Output: %s", string(output))
		}
		return false
	}
	log.Printf("[+] Command completed successfully")
	return true
}
