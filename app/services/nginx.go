package services

import (
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"proxy-manager/config"
	"regexp"
)

func NginxStatus() string {
	pidPath := config.GetNginxCompileInfo().NginxPidPath
	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result string
	if err != nil {
		result = "KO"
	} else {
		// 运行成功会读取 pid
		result = string(out)
	}
	return result
}

func StartNginx() string {
	_, err := exec.Command("nginx").CombinedOutput()
	var result string
	if err != nil {
		fmt.Println("启动出错")
		fmt.Println(err)
		result = "KO"
	} else {
		result = "OK"
	}
	return result
}

func ReloadNginx() string {
	if NginxStatus() != "KO" {
		out, err := exec.Command("nginx", "-s", "reload").CombinedOutput()
		var result string
		if err != nil {
			fmt.Println("重载配置出现错误")
			result = string(out)
		} else {
			result = "OK"
		}
		return result
	} else {
		fmt.Println("Nginx 没有启动,不用重载配置 !")
		return "KO"
	}
}

func StopNginx() string {
	_, err := exec.Command("nginx", "-s", "stop").CombinedOutput()
	var result string
	if err != nil {
		fmt.Println("停止出现错误")
		fmt.Println(err)
		result = "KO"
	} else {
		result = "OK"
	}
	return result
}

func GetNginxConfPath() string {
	configPath := config.GetNginxCompileInfo().NginxConfPath
	regex, _ := regexp.Compile("(.*)/(.*.conf)")
	confPath := regex.FindStringSubmatch(configPath)[1]
	log.Println("配置文件位置: " + confPath)
	return confPath
}

func SaveNginxConf(content string) {
	path := filepath.Join(GetNginxConfPath(), "nginx.conf")
	ioutil.WriteFile(path, []byte(content), 0644)
	ReloadNginx()
}
