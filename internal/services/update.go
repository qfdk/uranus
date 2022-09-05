package services

import (
	"fmt"
	"github.com/cheggaaa/pb/v3"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"uranus/internal/config"
)

var projectName = "uranus"

// CheckIfError ...
func checkIfError(err error) {
	if err == nil {
		return
	}
	fmt.Printf("\x1b[31;1m%s\x1b[0m\n", fmt.Sprintf("error: %s", err))
	os.Exit(1)
}

func ToUpdateProgram(url string) {
	// 拿到压缩包文件名
	client := http.DefaultClient
	client.Timeout = time.Second * 60 * 10
	resp, err := client.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	var downloadTarget = projectName + "-" + runtime.GOARCH
	if resp.StatusCode == http.StatusOK {
		log.Printf("[INFO] 获取更新: [%s]", downloadTarget)
		//_ = os.Rename(path.Join(config.GetAppConfig().InstallPath, projectName), path.Join(config.GetAppConfig().InstallPath, projectName+"_back"))
		newProjectName := path.Join(config.GetAppConfig().InstallPath, downloadTarget)
		log.Printf("[INFO] 下载位置 : %s", newProjectName)
		downFile, err := os.Create(newProjectName)
		checkIfError(err)
		defer downFile.Close()

		// 获取下载文件的大小
		contentLength, _ := strconv.Atoi(resp.Header.Get("Content-Length"))
		sourceSiz := int64(contentLength)
		source := resp.Body

		// 创建一个进度条
		bar := pb.Full.Start64(sourceSiz)
		bar.SetMaxWidth(100)
		barReader := bar.NewProxyReader(source)
		writer := io.MultiWriter(downFile)
		_, err = io.Copy(writer, barReader)
		bar.Finish()

		// 检查文件大小
		stat, _ := os.Stat(newProjectName)
		if stat.Size() != int64(contentLength) {
			log.Printf("[ERROR] [%s]更新失败", projectName)
			err = os.Remove(newProjectName)
			checkIfError(err)
		}
		log.Printf("[INFO] [%s] 下载成功, 准备下一步操作", downloadTarget)
		_ = os.Chmod(newProjectName, os.ModePerm)
		_ = os.Remove(path.Join(config.GetAppConfig().InstallPath, projectName))
		_ = os.Rename(path.Join(config.GetAppConfig().InstallPath, newProjectName), path.Join(config.GetAppConfig().InstallPath, projectName))
		syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	} else {
		log.Printf("[ERROR] [%s]更新失败", downloadTarget)
		_ = os.Remove(downloadTarget)
	}
}
