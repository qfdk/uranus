package services

import (
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"uranus/internal/config"
)

var sysType = runtime.GOOS

func NginxStatus() string {
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result = string(out)
	if err != nil {
		//log.Printf("[NGINX] 没有启动, %s", out)
		result = "KO"
	} else {
		result = string(out)
	}
	return result
}

func StartNginx() string {
	var out []byte
	var err error
	if sysType == "darwin" {
		out, err = exec.Command("nginx").CombinedOutput()
	} else {
		out, err = exec.Command("systemctl", "start", "nginx").CombinedOutput()
	}
	var result = "OK"
	if err != nil {
		result = err.Error()
		log.Printf("[PID][%d]: [NGINX] 启动失败, %s", syscall.Getpid(), out)
	} else {
		log.Printf("[PID][%d]: [NGINX] 启动成功", syscall.Getpid())
	}
	return result
}

func ReloadNginx() string {
	log.Println("[NGINX] 重载配置文件")
	var result = "OK"
	if NginxStatus() != "KO" {
		var out []byte
		var err error
		if sysType == "darwin" {
			out, err = exec.Command("nginx", "-s", "reload").CombinedOutput()
		} else {
			out, err = exec.Command("systemctl", "reload", "nginx").CombinedOutput()
		}
		if err != nil {
			log.Println("[NGINX] 重载配置出现错误")
			result = string(out)
		}
	} else {
		log.Println("[NGINX] 没有启动,不用重载配置 !")
	}
	return result
}

func StopNginx() string {
	log.Printf("[PID][%d]: [NGINX] 停止", syscall.Getpid())
	var err error
	if sysType == "darwin" {
		_, err = exec.Command("nginx", "-s", "stop").CombinedOutput()
	} else {
		_, err = exec.Command("systemctl", "stop", "nginx").CombinedOutput()
	}

	var result = "OK"
	if err != nil {
		log.Printf("[PID][%d]: [NGINX] 停止出现错误", syscall.Getpid())
		log.Println(err)
		result = "KO"
	}
	return result
}

func getNginxConfPath() string {
	configPath := config.ReadNginxCompileInfo().NginxConfPath
	regex, _ := regexp.Compile("(.*)/(.*.conf)")
	confPath := regex.FindStringSubmatch(configPath)[1]
	return confPath
}

func SaveNginxConf(content string) string {
	path := filepath.Join(getNginxConfPath(), "nginx.conf")
	ioutil.WriteFile(path, []byte(content), 0644)
	log.Println("[NGINX] 保存配置成功")
	return ReloadNginx()
}
