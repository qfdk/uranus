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
	pidPath := config.ReadNginxCompileInfo().NginxPidPath
	out, err := exec.Command("cat", pidPath).CombinedOutput()
	var result = string(out)
	if err != nil {
		result = "KO"
	}
	return result
}

func StartNginx() string {
	fmt.Println("启动 Nginx")
	out, err := exec.Command("nginx").CombinedOutput()
	var result = "OK"
	if err != nil {
		fmt.Println("启动出错")
		result = string(out)
		fmt.Println(result)
	}
	return result
}

func ReloadNginx() string {
	fmt.Println("重载 Nginx 配置文件")
	var result = "OK"
	if NginxStatus() != "KO" {
		out, err := exec.Command("nginx", "-s", "reload").CombinedOutput()
		if err != nil {
			fmt.Println("重载配置出现错误")
			result = string(out)
		}
	} else {
		fmt.Println("Nginx 没有启动,不用重载配置 !")
	}
	return result
}

func StopNginx() string {
	fmt.Println("停止 Nginx")
	_, err := exec.Command("nginx", "-s", "stop").CombinedOutput()
	var result = "OK"
	if err != nil {
		fmt.Println("停止出现错误")
		fmt.Println(err)
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

func SaveNginxConf(content string) {
	path := filepath.Join(getNginxConfPath(), "nginx.conf")
	ioutil.WriteFile(path, []byte(content), 0644)
	fmt.Println("保存 Nginx 配置成功")
	ReloadNginx()
}
