package services

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
)

// 系统配置常量
const (
	// 安装目录路径，与systemd服务配置保持一致
	installPath = "/etc/uranus"
	// 二进制文件名
	binaryName = "uranus"
	// 服务名称
	serviceName = "uranus.service"
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
		log.Printf("[INFO] 当前程序已备份到 %s", backupPath)
	}

	// 下载新版本
	if err := downloadFile(url, binaryPath); err != nil {
		// 下载失败时恢复备份
		log.Printf("[ERROR] 下载失败: %v, 正在恢复备份", err)
		if _, statErr := os.Stat(backupPath); statErr == nil {
			if mvErr := os.Rename(backupPath, binaryPath); mvErr != nil {
				log.Printf("[ERROR] 恢复备份失败: %v", mvErr)
			} else {
				log.Printf("[INFO] 备份已成功恢复")
			}
		}
		return err
	}

	// 设置执行权限
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	log.Printf("[INFO] 更新成功，准备重启服务...")

	// 在systemd环境下，使用systemctl重启服务
	return restartSystemdService()
}

// restartSystemdService 使用systemctl重启服务
func restartSystemdService() error {
	log.Printf("[INFO] 使用systemctl重启%s...", serviceName)

	cmd := exec.Command("systemctl", "restart", serviceName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("重启服务失败: %v, 输出: %s", err, output)
	}

	log.Printf("[INFO] 服务重启命令已执行: %s", strings.TrimSpace(string(output)))

	// 等待服务启动
	time.Sleep(3 * time.Second)

	// 检查服务状态
	statusCmd := exec.Command("systemctl", "is-active", serviceName)
	statusOutput, _ := statusCmd.CombinedOutput()
	status := strings.TrimSpace(string(statusOutput))

	if status == "active" {
		log.Println("[INFO] 服务已成功重启并处于活动状态")
	} else {
		log.Printf("[WARN] 服务可能未正确启动，当前状态: %s", status)

		// 获取服务详细状态
		detailCmd := exec.Command("systemctl", "status", serviceName)
		detailOutput, _ := detailCmd.CombinedOutput()
		log.Printf("[INFO] 服务状态详情:\n%s", string(detailOutput))
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
