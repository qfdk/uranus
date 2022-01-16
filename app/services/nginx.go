package services

import (
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
	println("启动 nginx")
	out, err := exec.Command("nginx").CombinedOutput()
	var result string
	if err != nil {
		println("启动出错")
		result = string(out)
		println(result)
	} else {
		result = "OK"
	}
	return result
}

func ReloadNginx() string {
	println("重载 nginx 配置文件")
	if NginxStatus() != "KO" {
		out, err := exec.Command("nginx", "-s", "reload").CombinedOutput()
		var result string
		if err != nil {
			println("重载配置出现错误")
			result = string(out)
		} else {
			result = "OK"
		}
		return result
	} else {
		println("Nginx 没有启动,不用重载配置 !")
		return "OK"
	}
}

func StopNginx() string {
	println("关闭 nginx")
	_, err := exec.Command("nginx", "-s", "stop").CombinedOutput()
	var result string
	if err != nil {
		println("停止出现错误")
		println(err)
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
	println("配置文件位置: " + confPath)
	return confPath
}

func SaveNginxConf(content string) {
	println("保存 Nginx 配置")
	path := filepath.Join(GetNginxConfPath(), "nginx.conf")
	ioutil.WriteFile(path, []byte(content), 0644)
	ReloadNginx()
}
