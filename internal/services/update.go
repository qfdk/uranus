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
	// 升级触发文件
	upgradeTrigger = ".upgrade_trigger"
)

// ToUpdateProgram 从指定URL下载并安装新版本程序
func ToUpdateProgram(url string) error {
	log.Println("[INFO] 开始从", url, "下载更新")

	// 检查安装目录是否存在
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("安装目录 %s 不存在", installPath)
	}

	// 准备路径
	binaryPath := path.Join(installPath, binaryName)
	backupPath := path.Join(installPath, fmt.Sprintf("%s.bak.%s", binaryName, time.Now().Format("20060102150405")))
	tempPath := path.Join(installPath, fmt.Sprintf("%s.new", binaryName))
	triggerPath := path.Join(installPath, upgradeTrigger)

	// 清理可能存在的旧临时文件
	os.Remove(tempPath)
	os.Remove(triggerPath)

	// 下载新版本到临时文件
	if err := downloadFile(url, tempPath); err != nil {
		return fmt.Errorf("下载失败: %v", err)
	}

	// 设置执行权限
	if err := os.Chmod(tempPath, 0755); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	// 备份当前可执行文件 (如果存在)
	if _, err := os.Stat(binaryPath); err == nil {
		if err = os.Rename(binaryPath, backupPath); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("备份当前可执行文件失败: %v", err)
		}
		log.Printf("[INFO] 当前程序已备份到 %s", backupPath)
	}

	// 将临时文件移动到目标位置
	if err := os.Rename(tempPath, binaryPath); err != nil {
		// 恢复备份
		if _, statErr := os.Stat(backupPath); statErr == nil {
			os.Rename(backupPath, binaryPath)
		}
		return fmt.Errorf("移动临时文件失败: %v", err)
	}

	log.Printf("[INFO] 更新成功，创建升级触发文件...")

	// 创建触发文件来告诉应用程序需要升级
	f, err := os.Create(triggerPath)
	if err != nil {
		log.Printf("[WARN] 无法创建升级触发文件: %v", err)
	} else {
		f.Close()
	}

	log.Printf("[INFO] 升级将在下一次服务检查周期完成")
	return nil
}

// downloadFile 从指定URL下载文件到目标路径，带进度条
func downloadFile(url, targetPath string) error {
	// 创建HTTP客户端，设置超时
	client := &http.Client{
		Timeout: time.Second * downloadTimeout,
	}

	// 发送请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	// 添加User-Agent以避免某些服务器拒绝请求
	req.Header.Set("User-Agent", "Uranus-Updater/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回错误状态码: %d", resp.StatusCode)
	}

	// 获取文件大小
	contentLength := int64(0)
	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr != "" {
		if length, err := strconv.ParseInt(contentLengthStr, 10, 64); err == nil {
			contentLength = length
		}
	}

	log.Printf("[INFO] 下载到临时位置: %s", targetPath)

	// 创建下载文件
	downFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("无法创建文件: %v", err)
	}
	defer downFile.Close()

	// 设置下载进度条
	var bar *pb.ProgressBar
	if contentLength > 0 {
		bar = pb.Full.Start64(contentLength)
		bar.SetMaxWidth(100)
		bar.Set(pb.Bytes, true)
		barReader := bar.NewProxyReader(resp.Body)

		// 下载文件
		if _, err := io.Copy(downFile, barReader); err != nil {
			return fmt.Errorf("下载过程中出错: %v", err)
		}
		bar.Finish()
	} else {
		// 如果无法获取文件大小，无进度条下载
		log.Printf("[INFO] 无法获取文件大小，下载开始...")
		if _, err := io.Copy(downFile, resp.Body); err != nil {
			return fmt.Errorf("下载过程中出错: %v", err)
		}
		log.Printf("[INFO] 下载完成")
	}

	return nil
}

// CheckAndRestartAfterUpgrade 检查是否需要重启服务
func CheckAndRestartAfterUpgrade() bool {
	triggerPath := path.Join(installPath, upgradeTrigger)

	// 检查触发文件是否存在
	if _, err := os.Stat(triggerPath); err == nil {
		log.Printf("[INFO] 检测到升级触发文件，准备重启服务...")
		// 删除触发文件
		os.Remove(triggerPath)

		// 尝试在systemd环境下重启服务
		if err := restartSystemdService(); err != nil {
			log.Printf("[ERROR] 重启服务失败: %v", err)
			return false
		}
		return true
	}
	return false
}

// restartSystemdService 使用systemctl重启服务
func restartSystemdService() error {
	log.Printf("[INFO] 使用systemctl重启%s...", serviceName)

	// 先检查服务是否存在
	checkCmd := exec.Command("systemctl", "list-unit-files", serviceName)
	checkOutput, err := checkCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(checkOutput), serviceName) {
		return fmt.Errorf("服务不存在或无法检查服务状态: %v", err)
	}

	// 尝试重启服务
	cmd := exec.Command("systemctl", "restart", serviceName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("重启服务失败: %v, 输出: %s", err, output)
	}

	log.Printf("[INFO] 服务重启命令已执行: %s", strings.TrimSpace(string(output)))
	return nil
}
