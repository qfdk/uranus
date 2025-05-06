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
		log.Fatalf("[-] Nginx doesn't seem to be installed: %s", err.Error())
		os.Exit(-1)
	}

	nginxCompileInfo := string(out)
	lines := strings.Split(nginxCompileInfo, "\n")

	// Create a new NginxCompileInfo struct
	nci := &NginxCompileInfo{}

	// Parse version
	versionRegex := regexp.MustCompile(`nginx version: (.+)`)
	if matches := versionRegex.FindStringSubmatch(lines[0]); len(matches) > 1 {
		nci.Version = matches[1]
	}

	// Parse compiler and SSL info based on format
	if strings.Contains(lines[1], "built with") {
		nci.CompilerVersion = "Non-compiled version"

		sslRegex := regexp.MustCompile(`built with (.+)`)
		if matches := sslRegex.FindStringSubmatch(lines[1]); len(matches) > 1 {
			nci.SSLVersion = matches[1]
		}

		nci.TLSSupport = lines[2]
	} else {
		compilerRegex := regexp.MustCompile(`built by (.+)`)
		if matches := compilerRegex.FindStringSubmatch(lines[1]); len(matches) > 1 {
			nci.CompilerVersion = matches[1]
		}

		sslRegex := regexp.MustCompile(`built with (.+)`)
		if matches := sslRegex.FindStringSubmatch(lines[2]); len(matches) > 1 {
			nci.SSLVersion = matches[1]
		}

		nci.TLSSupport = lines[3]
	}

	// Parse configure arguments in a single pass
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
