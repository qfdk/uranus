package services

import (
	"fmt"
	"io/ioutil"
	"log"
	"nginx-proxy-manager/app/config"
	"os/exec"
	"path/filepath"
	"regexp"
)

func NginxStatus() string {
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result = string(out)
	if err != nil {
		fmt.Println(result)
		result = "KO"
	}
	return result
}

func StartNginx() string {
	log.Println("启动 Nginx")
	out, err := exec.Command("systemctl", "start", "nginx-proxy-manager").CombinedOutput()
	var result = "OK"
	if err != nil {
		log.Println("启动出错")
		result = string(out)
		log.Println(result)
	}
	return result
}

func ReloadNginx() string {
	log.Println("重载 Nginx 配置文件")
	var result = "OK"
	if NginxStatus() != "KO" {
		out, err := exec.Command("nginx", "-s", "reload").CombinedOutput()
		if err != nil {
			log.Println("重载配置出现错误")
			result = string(out)
		}
	} else {
		log.Println("Nginx 没有启动,不用重载配置 !")
	}
	return result
}

func StopNginx() string {
	log.Println("停止 Nginx")
	out, err := exec.Command("systemctl", "stop", "nginx-proxy-manager").CombinedOutput()
	var result = "OK"
	if err != nil {
		log.Println("停止出现错误")
		log.Println(string(out))
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
	log.Println("保存 Nginx 配置成功")
	return ReloadNginx()
}
