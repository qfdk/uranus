package services

import (
	"os/exec"
	"fmt"
	"proxy-manager/config"
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
