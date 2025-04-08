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
	// 下载超时时间（秒）
	downloadTimeout = 600 // 10分钟

	// 升级信号类型
	// 0: 使用触发文件
	// 1: 使用 SIGHUP 信号
	// 2: 使用 SIGUSR2 信号
	upgradeMethod = 1
)

// ToUpdateProgram 从指定URL下载并安装新版本程序
func ToUpdateProgram(url string) error {
	pwd := getPWD()
	binaryPath := path.Join(pwd, "uranus")
	backupPath := path.Join(pwd, fmt.Sprintf("uranus.bak.%s", time.Now().Format("20060102150405")))
	pidFile := path.Join(pwd, "uranus.pid")

	log.Println("[INFO] 开始从", url, "下载更新")

	// 备份当前可执行文件
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

	log.Printf("[INFO] 更新成功，准备触发服务重启...")

	// 根据配置的升级方法触发重启
	switch upgradeMethod {
	case 0:
		// 创建触发文件方式
		return createTriggerFile(pwd)
	case 1, 2:
		// 信号方式
		return sendUpgradeSignal(pidFile, upgradeMethod)
	default:
		return fmt.Errorf("不支持的升级方法: %d", upgradeMethod)
	}
}

// createTriggerFile 创建升级触发文件
func createTriggerFile(pwd string) error {
	triggerPath := path.Join(pwd, ".upgrade_trigger")

	// 创建触发文件
	f, err := os.Create(triggerPath)
	if err != nil {
		return fmt.Errorf("创建升级触发文件失败: %v", err)
	}
	f.Close()

	log.Printf("[INFO] 已创建升级触发文件，服务将在检测到后自动重启")
	return nil
}

// sendUpgradeSignal 发送升级信号
func sendUpgradeSignal(pidFile string, signalType int) error {
	// 读取PID文件
	pidContent, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("无法读取PID文件 %s: %v", pidFile, err)
	}

	pid := strings.TrimSpace(string(pidContent))
	pidNum, err := strconv.Atoi(pid)
	if err != nil {
		return fmt.Errorf("PID文件内容无效 %s: %v", pid, err)
	}

	// 获取进程
	proc, err := os.FindProcess(pidNum)
	if err != nil {
		return fmt.Errorf("无法找到进程 %d: %v", pidNum, err)
	}

	// 发送相应的信号
	var sig syscall.Signal
	if signalType == 1 {
		sig = syscall.SIGHUP
		log.Printf("[INFO] 向进程 %d 发送 SIGHUP 信号", pidNum)
	} else {
		sig = syscall.SIGUSR2
		log.Printf("[INFO] 向进程 %d 发送 SIGUSR2 信号", pidNum)
	}

	if err := proc.Signal(sig); err != nil {
		return fmt.Errorf("发送信号失败: %v", err)
	}

	log.Printf("[INFO] 升级信号已发送，服务将自动重启")
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

// getPWD 获取当前工作目录（模拟tools.GetPWD()函数）
func getPWD() string {
	// 这里假设是在根目录部署的，实际使用时应该替换为您的tools.GetPWD()函数
	return "/etc/uranus"
}
