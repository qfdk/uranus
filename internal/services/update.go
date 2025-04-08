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

func ToUpdateProgram(url string) error {
	log.Println("[INFO] 开始从", url, "下载更新")

	// 创建HTTP客户端，设置超时
	client := &http.Client{
		Timeout: time.Second * 60 * 10,
	}

	// 发送请求
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回错误状态码: %d", resp.StatusCode)
	}

	// 获取文件大小
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		contentLength = 0 // 如果无法获取大小，设为0
	}

	// 准备文件路径
	installPath := config.GetAppConfig().InstallPath
	upgradedBinaryName := binaryName + "-" + runtime.GOARCH
	newFileWithFullPath := path.Join(installPath, upgradedBinaryName)
	originalBinaryPath := path.Join(installPath, binaryName)
	backupBinaryPath := path.Join(installPath, binaryName+".bak")

	log.Printf("[INFO] 下载位置: %s", newFileWithFullPath)

	// 创建下载文件
	downFile, err := os.Create(newFileWithFullPath)
	if err != nil {
		return fmt.Errorf("无法创建文件: %v", err)
	}
	defer downFile.Close()

	// 设置下载进度条
	bar := pb.Full.Start64(int64(contentLength))
	bar.SetMaxWidth(100)
	barReader := bar.NewProxyReader(resp.Body)

	// 下载文件
	bytesWritten, err := io.Copy(downFile, barReader)
	bar.Finish()

	if err != nil {
		os.Remove(newFileWithFullPath) // 清理临时文件
		return fmt.Errorf("下载过程中出错: %v", err)
	}

	// 验证文件大小
	if contentLength > 0 && bytesWritten != int64(contentLength) {
		os.Remove(newFileWithFullPath)
		return fmt.Errorf("下载的文件大小不匹配，期望 %d 字节，实际 %d 字节", contentLength, bytesWritten)
	}

	// 设置执行权限
	if err = os.Chmod(newFileWithFullPath, 0755); err != nil {
		os.Remove(newFileWithFullPath)
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	// 备份当前可执行文件
	if _, err := os.Stat(originalBinaryPath); err == nil {
		if err = os.Rename(originalBinaryPath, backupBinaryPath); err != nil {
			os.Remove(newFileWithFullPath)
			return fmt.Errorf("备份当前可执行文件失败: %v", err)
		}
	}

	// 移动新文件到目标位置
	if err = os.Rename(newFileWithFullPath, originalBinaryPath); err != nil {
		// 尝试恢复
		os.Rename(backupBinaryPath, originalBinaryPath)
		return fmt.Errorf("无法替换可执行文件: %v", err)
	}

	log.Printf("[INFO] 升级成功，正在重启服务...")

	// 使用更安全的方式重启
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return fmt.Errorf("无法获取当前进程: %v", err)
	}

	return proc.Signal(syscall.SIGHUP)
}
