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
	upgradedBinaryName := binaryName
	newFileWithFullPath := path.Join(installPath, upgradedBinaryName)
	backupBinaryPath := path.Join(installPath, binaryName+".bak")

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

	// 创建触发文件，不发送信号
	triggerFile := path.Join(tools.GetPWD(), ".upgrade_trigger")
	if err = os.WriteFile(triggerFile, []byte(fmt.Sprintf("upgrade=%d", time.Now().Unix())), 0644); err != nil {
		return fmt.Errorf("无法创建升级触发文件: %v", err)
	}

	log.Printf("[INFO] 升级准备完成，等待系统检测并重启 (通常在5秒内)")
	return nil
}
