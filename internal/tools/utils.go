package tools

import "os"

func GetPWD() string {
	pwd, err := os.Getwd()
	if err != nil || pwd == "/" {
		return "/etc/uranus"
	}
	return pwd
}
