package services

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"uranus/internal/config"
	"uranus/internal/models"
)

// DisableSite stops nginx from loading a site and keeps its configuration so
// it can be enabled again. A symbolic link (Debian's sites-enabled) is
// removed and its target kept, as a2dissite does; a file is moved to
// config.DisabledSitesDir. When nginx no longer accepts the configuration
// without the site (another file uses its upstream, say), the site stays
// enabled. Returns "OK" or why it failed.
func DisableSite(site config.SiteFile) string {
	record := models.DisabledSite{Path: site.Path}
	if site.Link != "" {
		target, err := os.Readlink(site.Path)
		if err != nil {
			return "读取链接失败：" + err.Error()
		}
		record.Link = target
		if err := os.Remove(site.Path); err != nil {
			return "删除链接失败：" + err.Error()
		}
	} else {
		dir := config.DisabledSitesDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "创建停用目录失败：" + err.Error()
		}
		// 不同目录里可能有同名的配置，加上时间区分
		record.Stored = filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+"-"+site.Name)
		if err := moveFile(site.Path, record.Stored); err != nil {
			return "移动配置文件失败：" + err.Error()
		}
	}

	restore := func() {
		if err := putBack(record); err != nil {
			log.Printf("[网站] 恢复 %s 失败: %v", site.Path, err)
		}
	}
	if nginxInstalled() {
		if msg := TestNginxConfig(); msg != "" {
			restore()
			return "去掉这个网站后配置检查不通过，网站没有停用：\n" + msg
		}
	}
	if err := models.GetDbClient().Create(&record).Error; err != nil {
		restore()
		return "记录停用的网站失败：" + err.Error()
	}
	log.Printf("[网站] 已停用 %s", site.Path)
	return ReloadNginx()
}

// EnableSite puts a disabled site back where nginx loads it. It stays
// disabled when its old place is taken or nginx rejects the configuration
// with it. Returns "OK" or why it failed.
func EnableSite(id uint) string {
	record, ok := models.FindDisabledSite(id)
	if !ok {
		return "没有这个停用的网站"
	}
	if _, err := os.Lstat(record.Path); err == nil {
		return "原来的位置 " + record.Path + " 已经有文件了，网站没有启用"
	}
	if err := os.MkdirAll(filepath.Dir(record.Path), 0o755); err != nil {
		return "创建目录失败：" + err.Error()
	}
	if err := putBack(record); err != nil {
		return "放回配置文件失败：" + err.Error()
	}

	if nginxInstalled() {
		if msg := TestNginxConfig(); msg != "" {
			if err := takeAway(record); err != nil {
				log.Printf("[网站] 撤回 %s 失败: %v", record.Path, err)
			}
			return "加上这个网站后配置检查不通过，网站仍然停用：\n" + msg
		}
	}
	if err := record.Forget(); err != nil {
		log.Printf("[网站] 删除停用记录失败: %v", err)
	}
	log.Printf("[网站] 已启用 %s", record.Path)
	return ReloadNginx()
}

// DeleteDisabledSite deletes a disabled site: its stored configuration file,
// or only the record when it was a link (the target stays, as when an
// enabled link is deleted).
func DeleteDisabledSite(record models.DisabledSite) error {
	if record.Stored != "" {
		if err := os.Remove(record.Stored); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return record.Forget()
}

// DisabledSiteConfig returns the configuration of a disabled site: its stored
// file, or the target of its link.
func DisabledSiteConfig(record models.DisabledSite) ([]byte, error) {
	if record.Stored != "" {
		return os.ReadFile(record.Stored)
	}
	target := record.Link
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(record.Path), target)
	}
	return os.ReadFile(target)
}

// putBack restores the link or file of a disabled site at its path.
func putBack(record models.DisabledSite) error {
	if record.Link != "" {
		return os.Symlink(record.Link, record.Path)
	}
	return moveFile(record.Stored, record.Path)
}

// takeAway undoes putBack.
func takeAway(record models.DisabledSite) error {
	if record.Link != "" {
		return os.Remove(record.Path)
	}
	return moveFile(record.Path, record.Stored)
}

// moveFile renames a file, or copies it and removes the original when the two
// paths are on different file systems.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(to)
		return err
	}
	if err := os.Remove(from); err != nil {
		os.Remove(to)
		return fmt.Errorf("删除原文件失败: %w", err)
	}
	return nil
}
