package tools

import "os"

func GetPWD() string {
	pwd, err := os.Getwd()
	if err != nil || pwd == "/" {
		return "/etc/uranus"
	}
	return pwd
}

// 检查文件是否存在
func FileExists(filepath string) (bool, error) {
	_, err := os.Stat(filepath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
