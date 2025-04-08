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
	// PID文件路径
	pidFilePath = "/etc/uranus/uranus.pid"
	// 升级触发文件路径
	triggerFilePath = "/etc/uranus/.upgrade_trigger"
	// 更新锁文件路径
	updateLockPath = "/etc/uranus/.update.lock"
	// 下载超时时间（秒）
	downloadTimeout = 600 // 10分钟
)

// ToUpdateProgram 从指定URL下载并安装新版本程序
// 下载完成后创建触发文件，等待服务检测并重启
func ToUpdateProgram(url string) error {
	log.Println("[INFO] 开始从", url, "下载更新")

	// 检查安装目录是否存在
	if _, err := os.Stat(installPath); os.IsNotExist(err) {
		return fmt.Errorf("安装目录 %s 不存在", installPath)
	}

	// 创建锁文件防止同时多次更新
	if err := createLockFile(); err != nil {
		return err
	}
	defer os.Remove(updateLockPath)

	// 下载新版本
	tmpFilePath := path.Join(installPath, binaryName+".tmp")
	_, err := downloadFile(url, tmpFilePath)
	if err != nil {
		os.Remove(tmpFilePath) // 清理临时文件
		return err
	}

	// 设置执行权限
	if err = os.Chmod(tmpFilePath, 0755); err != nil {
		os.Remove(tmpFilePath)
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	// 替换当前可执行文件
	if err = replaceExecutable(tmpFilePath); err != nil {
		return err
	}

	// 创建升级触发文件
	if err = createTriggerFile(); err != nil {
		return err
	}

	log.Printf("[INFO] 升级文件已准备就绪，等待服务在下次检查周期重启")
	return nil
}

// createLockFile 创建锁文件防止同时多次更新
func createLockFile() error {
	if _, err := os.Stat(updateLockPath); err == nil {
		return fmt.Errorf("另一个升级进程正在运行，请稍后再试")
	}

	content := fmt.Sprintf("pid=%d\ntime=%d\n", os.Getpid(), time.Now().Unix())
	if err := os.WriteFile(updateLockPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("无法创建锁文件: %v", err)
	}
	return nil
}

// downloadFile 从指定URL下载文件到目标路径
func downloadFile(url, targetPath string) (int, error) {
	// 创建HTTP客户端，设置超时
	client := &http.Client{
		Timeout: time.Second * downloadTimeout,
	}

	// 发送请求
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("下载失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("服务器返回错误状态码: %d", resp.StatusCode)
	}

	// 获取文件大小
	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		contentLength = 0 // 如果无法获取大小，设为0
		log.Println("[WARN] 无法获取文件大小，下载进度可能不准确")
	}

	log.Printf("[INFO] 下载位置: %s", targetPath)

	// 创建下载文件
	downFile, err := os.Create(targetPath)
	if err != nil {
		return 0, fmt.Errorf("无法创建临时文件: %v", err)
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
		return 0, fmt.Errorf("下载过程中出错: %v", err)
	}

	// 同步到磁盘
	if err = downFile.Sync(); err != nil {
		return 0, fmt.Errorf("无法将文件同步到磁盘: %v", err)
	}

	// 关闭文件
	if err = downFile.Close(); err != nil {
		return 0, fmt.Errorf("无法正确关闭文件: %v", err)
	}

	// 验证文件大小
	if contentLength > 0 && bytesWritten != int64(contentLength) {
		return 0, fmt.Errorf("下载的文件大小不匹配，期望 %d 字节，实际 %d 字节", contentLength, bytesWritten)
	}

	log.Printf("[INFO] 下载完成: %d 字节已写入磁盘", bytesWritten)
	return contentLength, nil
}

// replaceExecutable 替换当前可执行文件
func replaceExecutable(tmpFilePath string) error {
	binaryPath := path.Join(installPath, binaryName)
	backupBinaryPath := path.Join(installPath, binaryName+".bak")

	// 备份当前可执行文件
	if _, err := os.Stat(binaryPath); err == nil {
		if err = os.Rename(binaryPath, backupBinaryPath); err != nil {
			return fmt.Errorf("备份当前可执行文件失败: %v", err)
		}
		log.Printf("[INFO] 已备份当前可执行文件到 %s", backupBinaryPath)
	}

	// 移动新文件到目标位置
	if err := os.Rename(tmpFilePath, binaryPath); err != nil {
		// 尝试恢复
		os.Rename(backupBinaryPath, binaryPath)
		return fmt.Errorf("无法替换可执行文件: %v", err)
	}

	log.Printf("[INFO] 新版本可执行文件安装完成")
	return nil
}

// createTriggerFile 创建升级触发文件
func createTriggerFile() error {
	// 读取PID文件获取当前运行的主进程PID
	var mainPid string
	pidContent, err := os.ReadFile(pidFilePath)
	if err == nil {
		mainPid = strings.TrimSpace(string(pidContent))
		log.Printf("[INFO] 当前运行的进程PID: %s", mainPid)
	} else {
		log.Printf("[WARN] 无法读取PID文件: %v", err)
	}

	// 触发文件内容包含时间戳和进程信息
	triggerContent := fmt.Sprintf(
		"upgrade_time=%d\n"+
			"service=true\n"+
			"main_pid=%s\n"+
			"upgrader_pid=%d\n"+
			"version_hash=%s\n",
		time.Now().Unix(),
		mainPid,
		os.Getpid(),
		getVersionHash(), // 获取版本哈希，如果有的话
	)

	if err = os.WriteFile(triggerFilePath, []byte(triggerContent), 0644); err != nil {
		return fmt.Errorf("无法创建升级触发文件: %v", err)
	}

	log.Printf("[INFO] 已创建升级触发文件: %s", triggerFilePath)
	return nil
}

// getVersionHash 获取当前版本哈希，如果有的话
// 这个函数可以从版本信息文件或其他地方获取版本信息
func getVersionHash() string {
	// 这里可以实现获取版本哈希的逻辑
	// 例如：从版本文件读取，或执行命令获取git hash等
	return "unknown" // 默认返回unknown
}

// 以下是一些额外的辅助函数，可以根据需要启用

// RestartService 使用systemctl重启服务
func RestartService() error {
	log.Printf("[INFO] 正在请求systemd重启服务...")
	cmd := exec.Command("systemctl", "restart", "uranus.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("通过systemctl重启失败: %v, 输出: %s", err, string(out))
	}
	return nil
}

// CheckUpdate 检查是否有新版本可用
// 参数：当前版本号，检查更新的URL
func CheckUpdate(currentVersion, checkURL string) (hasUpdate bool, newVersion string, downloadURL string, err error) {
	// 这里实现检查更新的逻辑
	// 例如：向服务器发送GET请求，比较版本号等

	// 返回示例：
	// return true, "1.2.3", "https://example.com/download/uranus-1.2.3", nil

	// 默认实现，直接返回无更新
	return false, "", "", nil
}

// RollbackUpdate 回滚到备份版本
func RollbackUpdate() error {
	binaryPath := path.Join(installPath, binaryName)
	backupBinaryPath := path.Join(installPath, binaryName+".bak")

	// 检查备份文件是否存在
	if _, err := os.Stat(backupBinaryPath); os.IsNotExist(err) {
		return fmt.Errorf("备份文件不存在，无法回滚")
	}

	// 如果当前文件存在，先删除
	if _, err := os.Stat(binaryPath); err == nil {
		if err = os.Remove(binaryPath); err != nil {
			return fmt.Errorf("删除当前文件失败: %v", err)
		}
	}

	// 恢复备份文件
	if err := os.Rename(backupBinaryPath, binaryPath); err != nil {
		return fmt.Errorf("恢复备份文件失败: %v", err)
	}

	log.Printf("[INFO] 已成功回滚到备份版本")

	// 创建回滚触发文件
	rollbackTrigger := path.Join(installPath, ".rollback_trigger")
	if err := os.WriteFile(rollbackTrigger, []byte(fmt.Sprintf("rollback_time=%d\n", time.Now().Unix())), 0644); err != nil {
		log.Printf("[WARN] 创建回滚触发文件失败: %v", err)
	}

	return nil
}
