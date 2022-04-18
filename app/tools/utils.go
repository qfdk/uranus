package tools

import "os"

func GetPWD() string {
	pwd, _ := os.Getwd()
	if pwd == "/" {
		pwd = "/etc/uranus"
	}
	return pwd
}
