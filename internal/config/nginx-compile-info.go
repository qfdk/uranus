package config

import (
	"log"
	"os"
	"os/exec"
	"regexp"
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
	_nci     *NginxCompileInfo
	nciOnce  sync.Once
	nciMutex sync.RWMutex
)

// ReadNginxCompileInfo returns Nginx compilation information with thread-safe caching
func ReadNginxCompileInfo() *NginxCompileInfo {
	// Initialize once with sync.Once for thread safety
	nciOnce.Do(func() {
		initNginxCompileInfo()
	})

	// Use read lock for better concurrency
	nciMutex.RLock()
	defer nciMutex.RUnlock()

	return _nci
}

func initNginxCompileInfo() {
	// Execute nginx -V command to get compile info
	out, err := exec.Command("nginx", "-V").CombinedOutput()
	if err != nil {
		log.Printf("Error getting nginx configuration: %v\n", err)
		log.Printf("[-] Nginx doesn't seem to be installed: %s", err.Error())
		
		// 检查是否以root权限运行
		if os.Geteuid() != 0 {
			log.Printf("[!] Please install nginx manually or run as root for auto-installation:")
			log.Printf("    Ubuntu/Debian: sudo apt update && sudo apt install nginx")
			log.Printf("    CentOS/RHEL:   sudo yum install nginx")
			log.Printf("    Rocky/Alma:    sudo dnf install nginx")
		} else {
			log.Printf("[!] Attempting to auto-install nginx...")
			if autoInstallNginx() {
				log.Printf("[+] Nginx installed successfully! Re-initializing...")
				// 重新尝试获取nginx信息
				if out, err := exec.Command("nginx", "-V").CombinedOutput(); err == nil {
					log.Printf("[+] Nginx installation verified, continuing with initialization...")
					// 继续正常的初始化流程
					nginxCompileInfo := string(out)
					lines := strings.Split(nginxCompileInfo, "\n")
					parseNginxInfo(lines)
					return
				}
			}
			log.Printf("[-] Auto-installation failed. Please install nginx manually")
		}
		
		// Return empty info instead of crashing
		nciMutex.Lock()
		_nci = &NginxCompileInfo{
			Version:         "Not installed - Please install nginx",
			CompilerVersion: "N/A",
			SSLVersion:      "N/A", 
			TLSSupport:      "N/A",
			NginxExec:       "",
			NginxConfPath:   "",
			NginxPidPath:    "",
			Params:          []string{},
		}
		nciMutex.Unlock()
		return
	}

	nginxCompileInfo := string(out)
	lines := strings.Split(nginxCompileInfo, "\n")
	parseNginxInfo(lines)
}

// parseNginxInfo parses nginx -V output and updates the global nginx compile info
func parseNginxInfo(lines []string) {
	// Create a new NginxCompileInfo struct
	nci := &NginxCompileInfo{}

	if len(lines) == 0 {
		nciMutex.Lock()
		_nci = nci
		nciMutex.Unlock()
		return
	}

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
	nginxCompileInfo := strings.Join(lines, "\n")
	configRegex := regexp.MustCompile(`configure arguments: (.+)`)
	var configArgs string

	if matches := configRegex.FindStringSubmatch(nginxCompileInfo); len(matches) > 1 {
		configArgs = matches[1]
	}

	if configArgs != "" {
		// Split and process configure arguments
		params := strings.Split("--"+configArgs, "--")
		nci.Params = params[1:] // Skip first empty element

		// Extract paths from parameters
		for _, param := range nci.Params {
			param = strings.TrimSpace(param)

			if strings.HasPrefix(param, "sbin-path=") {
				nci.NginxExec = strings.TrimSpace(strings.Split(param, "=")[1])
			} else if strings.HasPrefix(param, "conf-path=") {
				nci.NginxConfPath = strings.TrimSpace(strings.Split(param, "=")[1])
			} else if strings.HasPrefix(param, "pid-path=") {
				nci.NginxPidPath = strings.TrimSpace(strings.Split(param, "=")[1])
			}
		}
	}

	// Set the cached value with write lock
	nciMutex.Lock()
	_nci = nci
	nciMutex.Unlock()
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
