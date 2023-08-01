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

var binaryName = "uranus"

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

	var upgradedBinaryName = binaryName + "-" + runtime.GOARCH
	if resp.StatusCode == http.StatusOK {
		log.Printf("[INFO] 获取更新: [%s]", upgradedBinaryName)
		newFileWithFullPath := path.Join(config.GetAppConfig().InstallPath, upgradedBinaryName)
		log.Printf("[INFO] 下载位置 : %s", newFileWithFullPath)
		downFile, err := os.Create(newFileWithFullPath)
		checkIfError(err)
		defer func(downFile *os.File) {
			_ = downFile.Close()
		}(downFile)

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
		stat, _ := os.Stat(newFileWithFullPath)
		if stat.Size() != int64(contentLength) {
			log.Printf("[ERROR] [%s]更新失败", binaryName)
			err = os.Remove(newFileWithFullPath)
			checkIfError(err)
		}
		log.Printf("[INFO] [%s] 下载成功, 准备下一步操作", upgradedBinaryName)
		_ = os.Chmod(newFileWithFullPath, os.ModePerm)
		err = os.Remove(path.Join(config.GetAppConfig().InstallPath, binaryName))
		if err != nil {
			log.Printf("删除旧程序失败: %s", err)
			return
		}
		err = os.Rename(newFileWithFullPath, path.Join(config.GetAppConfig().InstallPath, binaryName))
		if err != nil {
			log.Printf("重命名新程序失败: %s", err)
			return
		}
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	} else {
		log.Printf("[ERROR] [%s]更新失败", upgradedBinaryName)
		_ = os.Remove(upgradedBinaryName)
	}
}
