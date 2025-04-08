package services

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cheggaaa/pb/v3"
)

// 系统配置常量
const (
	// 安装目录路径，与systemd服务配置保持一致
	installPath = "/etc/uranus"
	// 二进制文件名
	binaryName = "uranus"
	// PID文件路径
	pidFilePath = "/etc/uranus/uranus.pid"
	// 下载超时时间（秒）
	downloadTimeout = 600 // 10分钟
)

// ToUpdateProgram 从指定URL下载并安装新版本程序
func ToUpdateProgram(url string) error {
	log.Println("[INFO] 开始从", url, "下载更新")

	// 检查安装目录是否存在
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("安装目录 %s 不存在", installPath)
	}

	// 备份当前可执行文件
	binaryPath := path.Join(installPath, binaryName)
	backupPath := path.Join(installPath, fmt.Sprintf("%s.bak.%s", binaryName, time.Now().Format("20060102150405")))

	if _, err := os.Stat(binaryPath); err == nil {
		if err = os.Rename(binaryPath, backupPath); err != nil {
			return fmt.Errorf("备份当前可执行文件失败: %v", err)
		}
	}

	// 下载新版本
	if err := downloadFile(url, binaryPath); err != nil {
		// 恢复备份
		if _, statErr := os.Stat(backupPath); statErr == nil {
			os.Rename(backupPath, binaryPath)
		}
		return err
	}

	// 设置执行权限
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	log.Printf("[INFO] 升级成功，正在重启服务...")

	// 读取PID文件获取当前运行的主进程PID
	pidContent, err := os.ReadFile(pidFilePath)
	if err == nil {
		pid := strings.TrimSpace(string(pidContent))
		// 向主进程发送SIGUSR1信号触发平滑重启
		pidNum, _ := strconv.Atoi(pid)
		if pidNum > 0 {
			proc, err := os.FindProcess(pidNum)
			if err == nil {
				proc.Signal(os.Signal(syscall.SIGUSR1))
			}
		}
	}

	return nil
}

// downloadFile 从指定URL下载文件到目标路径
func downloadFile(url, targetPath string) error {
	// 创建HTTP客户端，设置超时
	client := &http.Client{
		Timeout: time.Second * downloadTimeout,
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

	log.Printf("[INFO] 下载位置: %s", targetPath)

	// 创建下载文件
	downFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("无法创建文件: %v", err)
	}
	defer downFile.Close()

	// 设置下载进度条
	bar := pb.Full.Start64(int64(contentLength))
	bar.SetMaxWidth(100)
	barReader := bar.NewProxyReader(resp.Body)

	// 下载文件
	if _, err := io.Copy(downFile, barReader); err != nil {
		return fmt.Errorf("下载过程中出错: %v", err)
	}
	bar.Finish()

	return nil
}
