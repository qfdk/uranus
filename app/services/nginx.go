package services

import (
	"fmt"
	"log"
	"os/exec"
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
	out, err := exec.Command("nginx").CombinedOutput()
	var result string
	if err != nil {
		fmt.Println("启动出错")
		fmt.Println(err)
		result = "KO"
	} else {
		result = "OK"
	}
	fmt.Println(string(out))
	return result
}

func ReloadNginx() string {
	out, err := exec.Command("nginx", "-s", "reload").CombinedOutput()
	var result string
	if err != nil {
		fmt.Println("重载配置出现错误")
		fmt.Println(err)
		result = "KO"
	} else {
		result = "OK"
	}
	output := string(out)
	fmt.Println(output)
	return result
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

//func ParserNginxConfig(configPath string) string {
//	GetNginxConfPath()
//	return string("confPath")
//}
