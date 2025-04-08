package services

import (
	"fmt"
	"github.com/cheggaaa/pb/v3"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"
)

var binaryName = "uranus"

func ToUpdateProgram(url string) error {
	log.Println("[INFO] 开始从", url, "下载更新")

	// 确认当前执行环境
	log.Printf("[INFO] 当前进程 PID: %d, 父进程 PID: %d", os.Getpid(), os.Getppid())

	// 创建一个锁文件防止多个更新同时进行
	lockFile := path.Join(tools.GetPWD(), ".update.lock")
	if _, err := os.Stat(lockFile); err == nil {
		return fmt.Errorf("另一个升级进程正在运行，请稍后再试")
	}
	if err := os.WriteFile(lockFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		return fmt.Errorf("无法创建锁文件: %v", err)
	}
	defer os.Remove(lockFile)

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

	// 准备文件路径 - 确保服务有权限操作这些目录
	installPath := config.GetAppConfig().InstallPath
	upgradedBinaryName := binaryName
	newFileWithFullPath := path.Join(installPath, upgradedBinaryName)
	backupBinaryPath := path.Join(installPath, binaryName+".bak")

	// 检查目录权限
	if err := checkDirectoryPermissions(installPath); err != nil {
		return fmt.Errorf("权限检查失败: %v", err)
	}

	// 临时文件
	tmpFile := newFileWithFullPath + ".tmp"

	log.Printf("[INFO] 下载位置: %s", newFileWithFullPath)

	// 创建下载文件
	downFile, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("无法创建临时文件: %v", err)
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
		os.Remove(tmpFile) // 清理临时文件
		return fmt.Errorf("下载过程中出错: %v", err)
	}

	// 同步到磁盘
	if err = downFile.Sync(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("无法将文件同步到磁盘: %v", err)
	}

	// 关闭文件
	if err = downFile.Close(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("无法正确关闭文件: %v", err)
	}

	// 验证文件大小
	if contentLength > 0 && bytesWritten != int64(contentLength) {
		os.Remove(tmpFile)
		return fmt.Errorf("下载的文件大小不匹配，期望 %d 字节，实际 %d 字节", contentLength, bytesWritten)
	}

	// 设置执行权限
	if err = os.Chmod(tmpFile, 0755); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("无法设置执行权限: %v", err)
	}

	// 备份当前可执行文件
	if _, err := os.Stat(newFileWithFullPath); err == nil {
		if err = os.Rename(newFileWithFullPath, backupBinaryPath); err != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("备份当前可执行文件失败: %v", err)
		}
	}

	// 移动新文件到目标位置
	if err = os.Rename(tmpFile, newFileWithFullPath); err != nil {
		// 尝试恢复
		os.Rename(backupBinaryPath, newFileWithFullPath)
		return fmt.Errorf("无法替换可执行文件: %v", err)
	}

	log.Printf("[INFO] 升级文件准备就绪，触发平滑重启...")

	// 创建触发文件，包含时间戳信息
	triggerFile := path.Join(tools.GetPWD(), ".upgrade_trigger")
	triggerContent := fmt.Sprintf("upgrade=%d\nservice=true\npid=%d",
		time.Now().Unix(), os.Getpid())

	if err = os.WriteFile(triggerFile, []byte(triggerContent), 0644); err != nil {
		return fmt.Errorf("无法创建升级触发文件: %v", err)
	}

	log.Printf("[INFO] 升级准备完成，等待服务检测并重启 (通常在5秒内)")
	return nil
}

// 检查目录权限
func checkDirectoryPermissions(dir string) error {
	// 创建测试文件
	testFile := path.Join(dir, ".permission_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("写入权限检查失败: %v", err)
	}

	// 删除测试文件
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("删除权限检查失败: %v", err)
	}

	return nil
}
