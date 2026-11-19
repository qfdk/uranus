package services

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"uranus/internal/config"
	"uranus/internal/tools"

	"github.com/cheggaaa/pb/v3"
)

// ReleaseBaseURL 是发布文件所在的地址：各架构的程序 uranus-<arch>（amd64、arm64）、
// 安装/升级/卸载脚本，以及描述当前发布版本的 version.json。由 scripts/release.sh 上传。
const ReleaseBaseURL = "https://de.qfdk.me/uranus"

// releasePublicKey 验证 version.json 的签名。对应的私钥只在发布用的机器上
// （scripts/release.sh 用 cmd/sign-release 签名），发布服务器被人改了文件，节点也不会升级。
var releasePublicKey = "ieYl7B4CpP/4IQtS4HQkI1GodYMNLijogoYHBfs90lI="

var (
	// releaseURL 是取发布文件的地址，测试时换成本地服务
	releaseURL = ReleaseBaseURL
	// installPath 是安装目录，与 systemd 服务配置一致；测试时换成临时目录
	installPath = "/etc/uranus"
)

// 系统配置常量
const (
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
	if sysType != "linux" {
		return fmt.Errorf("在线升级仅支持 Linux（当前系统: %s），请手动更新", sysType)
	}
	if _, err := os.Stat(installPath); err != nil {
		return fmt.Errorf("安装目录 %s 不存在，无法在线升级", installPath)
	}
	return nil
}

// Manifest describes the published release: version.json, signed by the
// release machine.
type Manifest struct {
	CommitID     string `json:"commitId"`
	BuildTime    string `json:"buildTime"`
	BuildVersion string `json:"buildVersion"`
	// Release counts the commits of the release, so a larger one is newer.
	Release int                     `json:"release"`
	Files   map[string]ManifestFile `json:"files"`
}

// ManifestFile is one published program.
type ManifestFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// FetchManifest downloads version.json and accepts it only with a valid
// signature.
func FetchManifest() (*Manifest, error) {
	body, err := fetchSmall(releaseURL + "/version.json")
	if err != nil {
		return nil, err
	}
	sigText, err := fetchSmall(releaseURL + "/version.json.sig")
	if err != nil {
		return nil, fmt.Errorf("发布清单没有签名: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	key, _ := base64.StdEncoding.DecodeString(releasePublicKey)
	if err != nil || !ed25519.Verify(key, body, sig) {
		return nil, fmt.Errorf("发布清单的签名不对，不升级")
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("解析发布清单失败: %v", err)
	}
	return &manifest, nil
}

func fetchSmall(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s 返回 %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Newer reports whether the release is newer than the running program. Builds
// without a release number (development builds, old manifests) compare commits.
func (m *Manifest) Newer() bool {
	running, _ := strconv.Atoi(config.Release)
	if m.Release == 0 || running == 0 {
		return m.CommitID != config.CommitID
	}
	return m.Release > running
}

// ToUpdateProgram 下载并安装新版本：发布清单的签名、程序的 sha256 和大小都要对得上，
// 新程序在这台机器上试运行通过后才替换。当前程序保留为 uranus.previous，新版本起不来时换回它
func ToUpdateProgram() error {
	if err := CheckSelfUpdate(); err != nil {
		return err
	}
	manifest, err := FetchManifest()
	if err != nil {
		return err
	}
	if !manifest.Newer() {
		return fmt.Errorf("已是最新版本")
	}
	name := binaryName + "-" + runtime.GOARCH
	file, ok := manifest.Files[name]
	if !ok {
		return fmt.Errorf("发布清单里没有 %s", name)
	}

	binaryPath := path.Join(installPath, binaryName)
	newPath := binaryPath + ".new"
	os.Remove(newPath)
	os.Remove(path.Join(installPath, upgradeTrigger))
	log.Printf("[升级] 下载 %s（%s）", name, short(manifest.CommitID))
	if err := downloadFile(releaseURL+"/"+name, newPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("下载失败: %v", err)
	}
	if err := verifyFile(newPath, file); err != nil {
		os.Remove(newPath)
		return err
	}
	if err := os.Chmod(newPath, 0755); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("无法设置执行权限: %v", err)
	}
	if err := preflight(newPath, manifest.CommitID); err != nil {
		os.Remove(newPath)
		return err
	}

	// 先复制一份当前程序，再整体替换：替换的瞬间程序文件始终存在
	if err := copyFile(binaryPath, binaryPath+".previous"); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("备份当前程序失败: %v", err)
	}
	if err := writePendingUpgrade(pendingUpgrade{Commit: manifest.CommitID}); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("记录升级状态失败: %v", err)
	}
	if err := os.Rename(newPath, binaryPath); err != nil {
		os.Remove(pendingUpgradePath())
		os.Remove(newPath)
		return fmt.Errorf("替换程序失败: %v", err)
	}

	// 触发文件让运行中的进程重启服务
	if f, err := os.Create(path.Join(installPath, upgradeTrigger)); err != nil {
		log.Printf("[WARN] 无法创建升级触发文件: %v", err)
	} else {
		f.Close()
	}
	log.Printf("[升级] 已换上 %s，即将重启", short(manifest.CommitID))
	return nil
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// verifyFile checks a download against the signed manifest.
func verifyFile(file string, want ManifestFile) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if size != want.Size || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		return fmt.Errorf("下载的程序与发布清单不一致（可能不完整或被改过），不升级")
	}
	return nil
}

// preflight runs the new program with --version: it has to start on this
// machine (architecture, glibc) and be the build the manifest names.
func preflight(binary, commit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("新版本无法在这台机器上运行，不升级: %v %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), commit) {
		return fmt.Errorf("新版本的提交号与发布清单不一致，不升级")
	}
	return nil
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := to + ".tmp"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

// 升级后的观察期：新版本连续这么多次没能正常启动，或者一次启动这么久还没就绪，就换回上一版
const (
	maxUpgradeAttempts = 3
	upgradeProbation   = 90 * time.Second
	pendingUpgradeFile = ".upgrade_pending"
)

type pendingUpgrade struct {
	Commit   string `json:"commit"`
	Attempts int    `json:"attempts"`
}

func pendingUpgradePath() string {
	return path.Join(installPath, pendingUpgradeFile)
}

func writePendingUpgrade(p pendingUpgrade) error {
	data, _ := json.Marshal(p)
	return os.WriteFile(pendingUpgradePath(), data, 0600)
}

// upgradeConfirmed is set once the upgraded program is ready.
var upgradeConfirmed atomic.Bool

// exit ends the process; tests replace it.
var exit = os.Exit

// CheckPendingUpgrade runs first at startup. Right after an upgrade the new
// program is on probation: when it failed to start three times, the previous
// program is put back and the process exits, so that systemd starts that one.
// A start that does not get ready within 90 seconds counts as failed.
func CheckPendingUpgrade() {
	data, err := os.ReadFile(pendingUpgradePath())
	if err != nil {
		return
	}
	var p pendingUpgrade
	_ = json.Unmarshal(data, &p)
	p.Attempts++
	if p.Attempts > maxUpgradeAttempts {
		rollbackUpgrade(fmt.Sprintf("新版本 %s 连续 %d 次没有正常启动", short(p.Commit), maxUpgradeAttempts))
		return
	}
	if err := writePendingUpgrade(p); err != nil {
		log.Printf("[升级] 记录启动次数失败: %v", err)
	}
	log.Printf("[升级] 新版本 %s 第 %d 次启动，就绪后确认升级", short(p.Commit), p.Attempts)
	go func() {
		time.Sleep(upgradeProbation)
		if !upgradeConfirmed.Load() {
			log.Printf("[升级] 新版本 %v 内没有就绪，退出重试", upgradeProbation)
			exit(1)
		}
	}()
}

// ConfirmUpgrade is called once the program is ready: the upgrade worked.
func ConfirmUpgrade() {
	upgradeConfirmed.Store(true)
	if err := os.Remove(pendingUpgradePath()); err == nil {
		log.Printf("[升级] 新版本运行正常，升级完成")
	}
}

func rollbackUpgrade(reason string) {
	os.Remove(pendingUpgradePath())
	previous := path.Join(installPath, binaryName+".previous")
	if _, err := os.Stat(previous); err != nil {
		log.Printf("[升级] %s，但没有上一版可以换回", reason)
		return
	}
	if err := copyFile(previous, path.Join(installPath, binaryName)); err != nil {
		log.Printf("[升级] %s，换回上一版失败: %v", reason, err)
		return
	}
	log.Printf("[升级] %s，已换回上一版，退出后由 systemd 重新启动", reason)
	exit(1)
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
