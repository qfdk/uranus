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
	// NginxExec is the executable Uranus runs, NginxConfPath the main
	// configuration file it runs with (the -c of the systemd unit, otherwise
	// the compiled-in path) and NginxPidPath the compiled-in pid file; the pid
	// directive of nginx.conf overrides it, see NginxPidFile.
	NginxExec     string
	NginxConfPath string
	NginxPidPath  string
	Prefix        string
	Params        []string
}

var (
	_nci         *NginxCompileInfo
	nciInstalled bool
	nciMutex     sync.Mutex
	missingOnce  sync.Once

	// nginxSearchPaths 是 PATH 中找不到 nginx 时依次尝试的位置。
	// 例如 macOS 上由 launchd 启动时 PATH 不包含 Homebrew 目录，OpenResty 不把 nginx 放进 PATH。
	nginxSearchPaths = searchPathsFor(runtime.GOOS)
)

func searchPathsFor(goos string) []string {
	if goos == "darwin" {
		return []string{
			"/opt/homebrew/bin/nginx", // Homebrew（Apple Silicon）
			"/usr/local/bin/nginx",    // Homebrew（Intel）
			"/opt/local/sbin/nginx",   // MacPorts
			"/opt/homebrew/opt/openresty/nginx/sbin/nginx",
			"/usr/local/opt/openresty/nginx/sbin/nginx",
			"/usr/local/nginx/sbin/nginx", // 源码编译的默认位置
			"/usr/local/openresty/nginx/sbin/nginx",
		}
	}
	return []string{
		"/usr/sbin/nginx", // 发行版和 nginx.org 的软件包
		"/usr/bin/nginx",  // Arch
		"/usr/local/sbin/nginx",
		"/usr/local/nginx/sbin/nginx",           // 源码编译的默认位置
		"/usr/local/openresty/nginx/sbin/nginx", // OpenResty
		"/opt/nginx/sbin/nginx",                 // Passenger
		"/home/linuxbrew/.linuxbrew/bin/nginx",  // Homebrew on Linux
	}
}

// NginxBinary returns the nginx executable Uranus runs.
func NginxBinary() string {
	if exe := ReadNginxCompileInfo().NginxExec; exe != "" {
		return exe
	}
	return "nginx"
}

// findNginx returns the nginx executable and, on Linux hosts where nginx is a
// systemd service, that unit. The program the unit starts is the nginx that
// serves the sites, whatever else is installed or comes first on PATH.
func findNginx() (string, SystemdUnit) {
	if hostOS == "linux" {
		if unit, ok := FindNginxUnit(); ok && isNginxProgram(unit.ExecPath) && isExecutable(unit.ExecPath) {
			return unit.ExecPath, unit
		}
	}
	return lookupNginx(), SystemdUnit{}
}

// lookupNginx finds nginx (or OpenResty) on PATH, otherwise in the usual
// install locations of this system.
func lookupNginx() string {
	for _, name := range []string{"nginx", "openresty"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range nginxSearchPaths {
		if isExecutable(p) {
			return p
		}
	}
	return "nginx"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// isNginxProgram tells nginx itself from a wrapper script a unit may start.
func isNginxProgram(path string) bool {
	name := filepath.Base(path)
	return name == "nginx" || name == "openresty"
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
	binary, unit := findNginx()
	out, err := exec.Command(binary, "-V").CombinedOutput()
	if err != nil {
		// 只在第一次发现缺少 nginx 时提示
		missingOnce.Do(func() { logMissingNginx(err) })
		return notInstalledInfo(), false
	}

	info := parseNginxInfo(string(out))
	info.NginxExec = binary
	// systemd 服务用 -c 指定了主配置时，nginx 实际读的是那个文件
	if unit.ConfArg != "" {
		info.NginxConfPath = underPrefix(info.Prefix, unit.ConfArg, "")
	}
	service := "无"
	if unit.Name != "" {
		service = unit.Name
	}
	log.Printf("[+] Nginx: %s, main config: %s, systemd unit: %s", info.NginxExec, info.NginxConfPath, service)
	return info, true
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

// logMissingNginx says how to install nginx. Uranus never installs it by
// itself: an nginx installed where detection does not look would end up
// installed twice. The install script installs nginx along with Uranus.
func logMissingNginx(cause error) {
	log.Printf("[-] 没有找到 nginx：%v", cause)
	if runtime.GOOS == "darwin" {
		log.Printf("[!] 用 Homebrew 安装：brew install nginx")
		return
	}
	log.Printf("[!] 安装 nginx 后刷新页面即可，例如 Debian/Ubuntu：apt install nginx，" +
		"CentOS/Rocky/Alma：dnf install nginx，Alpine：apk add nginx，Arch：pacman -S nginx")
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
	nci.Prefix = prefix
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
