package services

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
	"uranus/internal/tools"

	"github.com/cheggaaa/pb/v3"
)

// 系统配置常量
const (
	// 安装目录路径，与systemd服务配置保持一致
	installPath = "/etc/uranus"
	// 二进制文件名
	binaryName = "uranus"
	// 下载超时时间（秒）
	downloadTimeout = 600 // 10分钟
	// 升级触发文件
	upgradeTrigger = ".upgrade_trigger"
)

// CheckSelfUpdate 检查本机能否在线升级：发布的二进制是 Linux 版本，升级后靠 systemd 重启，
// 并且只替换标准安装目录中的程序。macOS 等其他系统需要手动更新。
func CheckSelfUpdate() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("在线升级仅支持 Linux（当前系统: %s），请手动更新", runtime.GOOS)
	}
	if _, err := os.Stat(installPath); err != nil {
		return fmt.Errorf("安装目录 %s 不存在，无法在线升级", installPath)
	}
	return nil
}

// ToUpdateProgram 从指定URL下载并安装新版本程序
func ToUpdateProgram(url string) error {
	if err := CheckSelfUpdate(); err != nil {
		return err
	}
	log.Println("[INFO] 开始从", url, "下载更新")

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

	// 清理所有旧备份文件，包括刚刚创建的备份
	DeleteAllBackups()

	log.Printf("[INFO] 升级将在下一次服务检查周期完成")
	return nil
}

// DeleteAllBackups 删除所有备份文件
func DeleteAllBackups() {
	log.Printf("[INFO] 开始清理所有备份文件...")

	// 获取安装目录中所有的备份文件
	backupFiles, err := filepath.Glob(path.Join(installPath, fmt.Sprintf("%s.bak.*", binaryName)))
	if err != nil {
		log.Printf("[WARN] 无法搜索备份文件: %v", err)
		return
	}

	// 如果没有找到备份文件
	if len(backupFiles) == 0 {
		log.Printf("[INFO] 未找到备份文件，无需清理")
		return
	}

	// 删除所有找到的备份文件
	for _, file := range backupFiles {
		log.Printf("[INFO] 删除备份文件: %s", file)
		if err := os.Remove(file); err != nil {
			log.Printf("[WARN] 删除文件失败 %s: %v", file, err)
		}
	}

	log.Printf("[INFO] 备份文件清理完成，共删除 %d 个文件", len(backupFiles))

	// 查找工作目录中的备份文件（以防安装在非标准位置）
	workingDir := tools.GetPWD()
	if workingDir != installPath {
		workingDirBackups, err := filepath.Glob(path.Join(workingDir, fmt.Sprintf("%s.bak.*", binaryName)))
		if err != nil {
			log.Printf("[WARN] 无法搜索工作目录备份文件: %v", err)
			return
		}

		// 删除工作目录中的备份文件
		for _, file := range workingDirBackups {
			log.Printf("[INFO] 删除工作目录备份文件: %s", file)
			if err := os.Remove(file); err != nil {
				log.Printf("[WARN] 删除文件失败 %s: %v", file, err)
			}
		}

		if len(workingDirBackups) > 0 {
			log.Printf("[INFO] 工作目录备份文件清理完成，共删除 %d 个文件", len(workingDirBackups))
		}
	}
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
