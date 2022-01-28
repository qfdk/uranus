package services

import (
	"fmt"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"io/ioutil"
	"os/exec"
	"path/filepath"
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
	fmt.Println("启动 Nginx")
	out, err := exec.Command("nginx").CombinedOutput()
	var result string
	if err != nil {
		fmt.Println("启动出错")
		result = string(out)
		fmt.Println(result)
	} else {
		result = "OK"
	}
	return result
}

func ReloadNginx() string {
	fmt.Println("重载 Nginx 配置文件")
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
		return "OK"
	}
}

func StopNginx() string {
	fmt.Println("停止 Nginx")
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

func getNginxConfPath() string {
	configPath := config.GetNginxCompileInfo().NginxConfPath
	regex, _ := regexp.Compile("(.*)/(.*.conf)")
	confPath := regex.FindStringSubmatch(configPath)[1]
	return confPath
}

func SaveNginxConf(content string) {
	path := filepath.Join(getNginxConfPath(), "nginx.conf")
	ioutil.WriteFile(path, []byte(content), 0644)
	fmt.Println("保存 Nginx 配置成功")
	ReloadNginx()
}
