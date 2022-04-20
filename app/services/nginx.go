package services

import (
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"uranus/app/config"
)

func NginxStatus() string {
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result = string(out)
	if err != nil {
		log.Println("PID 找不到")
		log.Println(err)
		result = "KO"
	}
	return result
}

func StartNginx() string {
	log.Printf("[PID][%d]: [Nginx] 启动", syscall.Getpid())
	out, err := exec.Command("systemctl", "start", "nginx").CombinedOutput()
	var result = "OK"
	if err != nil {
		result = err.Error()
		log.Println(result)
		log.Println(out)
	}
	return result
}

func ReloadNginx() string {
	log.Println("[Nginx] 重载配置文件")
	var result = "OK"
	if NginxStatus() != "KO" {
		out, err := exec.Command("systemctl", "reload", "nginx").CombinedOutput()
		if err != nil {
			log.Println("[Nginx] 重载配置出现错误")
			result = string(out)
		}
	} else {
		log.Println("[Nginx] 没有启动,不用重载配置 !")
	}
	return result
}

func StopNginx() string {
	log.Printf("[PID][%d]: [Nginx] 停止", syscall.Getpid())
	_, err := exec.Command("systemctl", "stop", "nginx").CombinedOutput()
	var result = "OK"
	if err != nil {
		log.Println("[Nginx] 停止出现错误")
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
	log.Println("[Nginx] 保存配置成功")
	return ReloadNginx()
}
