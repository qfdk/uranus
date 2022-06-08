package config

import (
	"log"
	"os"
	"os/exec"
	"strings"
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

var _nci *NginxCompileInfo = nil

func ReadNginxCompileInfo() *NginxCompileInfo {
	if _nci == nil {
		initNginxCompileInfo()
	}
	return _nci
}

func initNginxCompileInfo() {
	out, err := exec.Command("nginx", "-V").CombinedOutput()
	if err != nil {
		log.Printf("获取nginx配置出现错误,%v\n", err)
		log.Fatalf("[-] nginx 似乎没有安装, " + err.Error())
		os.Exit(-1)
	}
	nginxCompileInfo := string(out)
	arr := strings.Split(nginxCompileInfo, "\n")

	var nci = &NginxCompileInfo{}
	nci.Version = strings.Split(arr[0], "nginx version: ")[1]

	if strings.Contains(arr[1], "built with") {
		nci.CompilerVersion = "非编译版"
		nci.SSLVersion = strings.Split(arr[1], "built with ")[1]
		nci.TLSSupport = arr[2]
	} else {
		nci.CompilerVersion = strings.Split(arr[1], "built by ")[1]
		nci.SSLVersion = strings.Split(arr[2], "built with ")[1]
		nci.TLSSupport = arr[3]
	}

	for _, v := range arr {
		if strings.Contains(v, "configure arguments: ") {
			// 分割字符串
			arr := strings.Split(v, "configure arguments: --")
			// 第二部分 按照空格分割
			params := strings.Split(arr[1], "--")
			for _, param := range params {
				if strings.Contains(param, "sbin-path") {
					nci.NginxExec = strings.TrimSpace(strings.Split(param, "=")[1])
				}
				if strings.Contains(param, "conf-path") {
					nci.NginxConfPath = strings.TrimSpace(strings.Split(param, "=")[1])
				}
				if strings.Contains(param, "pid-path") {
					nci.NginxPidPath = strings.TrimSpace(strings.Split(param, "=")[1])
				}
			}
			nci.Params = params
		}
	}
	_nci = nci
}
