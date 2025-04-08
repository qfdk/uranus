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

	// 更新模式
	// 0: 使用触发文件
	// 1: 使用 SIGHUP 信号
	// 2: 使用 SIGUSR2 信号
	// 3: 使用 systemctl 重启服务
	updateMode = 3
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

	// 根据更新模式选择重启方式
	switch updateMode {
	case 0:
		return createTriggerFile()
	case 1, 2:
		sig := syscall.SIGHUP
		if updateMode == 2 {
			sig = syscall.SIGUSR2
		}
		return sendSignalToProcess(sig)
	case 3:
		return restartSystemdService()
	default:
		return fmt.Errorf("不支持的更新模式: %d", updateMode)
	}
}

// createTriggerFile 创建触发文件以触发程序自动重启
func createTriggerFile() error {
	triggerPath := path.Join(installPath, ".upgrade_trigger")

	f, err := os.Create(triggerPath)
	if err != nil {
		return fmt.Errorf("创建升级触发文件失败: %v", err)
	}
	f.Close()

	log.Printf("[INFO] 已创建升级触发文件，服务将在检测到后自动重启")
	return nil
}

// sendSignalToProcess 向主进程发送信号
func sendSignalToProcess(sig syscall.Signal) error {
	// 读取PID文件获取当前运行的主进程PID
	pidContent, err := os.ReadFile(pidFilePath)
	if err != nil {
		return fmt.Errorf("无法读取PID文件: %v", err)
	}

	pid := strings.TrimSpace(string(pidContent))
	pidNum, err := strconv.Atoi(pid)
	if err != nil {
		return fmt.Errorf("无效的PID值: %v", err)
	}

	sigName := "SIGHUP"
	if sig == syscall.SIGUSR2 {
		sigName = "SIGUSR2"
	}
	log.Printf("[INFO] 向PID %d 发送%s信号", pidNum, sigName)

	// 向主进程发送信号
	proc, err := os.FindProcess(pidNum)
	if err != nil {
		return fmt.Errorf("找不到进程PID %d: %v", pidNum, err)
	}

	if err := proc.Signal(sig); err != nil {
		return fmt.Errorf("发送信号失败: %v", err)
	}

	log.Println("[INFO] 升级信号已发送，服务将重启")
	return nil
}

// restartSystemdService 使用systemctl重启服务
func restartSystemdService() error {
	log.Println("[INFO] 使用systemctl重启uranus服务...")

	cmd := exec.Command("systemctl", "restart", "uranus.service")
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("重启服务失败: %v, 输出: %s", err, output)
	}

	log.Printf("[INFO] 服务重启命令已执行: %s", strings.TrimSpace(string(output)))

	// 等待服务启动
	time.Sleep(2 * time.Second)

	// 检查服务状态
	statusCmd := exec.Command("systemctl", "is-active", "uranus.service")
	statusOutput, _ := statusCmd.CombinedOutput()
	status := strings.TrimSpace(string(statusOutput))

	if status == "active" {
		log.Println("[INFO] 服务已成功重启并处于活动状态")
	} else {
		log.Printf("[WARN] 服务可能未正确启动，当前状态: %s", status)
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
