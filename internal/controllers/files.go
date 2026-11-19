package controllers

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"uranus/internal/config"
	"uranus/internal/services"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
)

// maxEditableSize is the largest file the editor opens.
const maxEditableSize = 1 << 20

// confCrumb is one directory on the way to a file of the nginx configuration.
type confCrumb struct {
	Name string
	Rel  string
}

// confCrumbs returns the directories leading to rel, without the
// configuration directory itself.
func confCrumbs(rel string) []confCrumb {
	crumbs := []confCrumb{}
	if rel == "" {
		return crumbs
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		crumbs = append(crumbs, confCrumb{Name: part, Rel: strings.Join(parts[:i+1], "/")})
	}
	return crumbs
}

// confError explains why a path of the configuration directory can't be opened.
func confError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "没有这个文件或目录，可能已经被删除或改名"
	case errors.Is(err, config.ErrOutsideConfDir):
		return "这个链接指向 Nginx 配置目录以外，不能在这里打开"
	default:
		return err.Error()
	}
}

type confRow struct {
	config.ConfFile
	SizeText string
}

// ConfFiles lists a directory of the nginx configuration.
func ConfFiles(ctx *gin.Context) {
	rel := config.ConfRel(ctx.Query("dir"))
	data := gin.H{
		"activePage": "config",
		"confDir":    config.NginxConfDir(),
		"rel":        rel,
		"crumbs":     confCrumbs(rel),
		"error":      ctx.Query("error"),
	}
	files, err := config.ListConfDir(rel)
	if err != nil {
		data["error"] = confError(err)
	}
	rows := []confRow{}
	for _, f := range files {
		row := confRow{ConfFile: f}
		if !f.IsDir {
			row.SizeText = humanize.Bytes(uint64(f.Size))
		}
		rows = append(rows, row)
	}
	data["files"] = rows
	ctx.HTML(http.StatusOK, "files.html", data)
}

// backToDir shows the directory of rel with an error.
func backToDir(ctx *gin.Context, rel, message string) {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	query := url.Values{"dir": {dir}, "error": {message}}
	ctx.Redirect(http.StatusFound, "/admin/nginx/files?"+query.Encode())
}

// EditConfFile opens a file of the nginx configuration in the editor.
func EditConfFile(ctx *gin.Context) {
	rel := config.ConfRel(ctx.Query("path"))
	abs, err := config.ConfDirPath(rel)
	if err != nil {
		backToDir(ctx, rel, confError(err))
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		backToDir(ctx, rel, confError(err))
		return
	}
	if info.IsDir() {
		ctx.Redirect(http.StatusFound, "/admin/nginx/files?"+url.Values{"dir": {rel}}.Encode())
		return
	}
	if info.Size() > maxEditableSize {
		backToDir(ctx, rel, path.Base(rel)+" 有 "+humanize.Bytes(uint64(info.Size()))+"，太大了，不能在这里编辑")
		return
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		backToDir(ctx, rel, confError(err))
		return
	}
	if !utf8.Valid(content) {
		backToDir(ctx, rel, path.Base(rel)+" 不是文本文件")
		return
	}
	link, _ := os.Readlink(abs)
	parent := path.Dir(rel)
	if parent == "." {
		parent = ""
	}
	ctx.HTML(http.StatusOK, "fileEdit.html", gin.H{
		"activePage": "config",
		"confDir":    config.NginxConfDir(),
		"rel":        rel,
		"parent":     parent,
		"name":       path.Base(rel),
		"abs":        abs,
		"link":       link,
		"content":    string(content),
	})
}

// SaveConfFile saves a file of the nginx configuration: nginx -t checks the
// whole configuration first, and a refused change is not kept.
func SaveConfFile(ctx *gin.Context) {
	rel := config.ConfRel(ctx.PostForm("path"))
	abs, err := config.ConfDirPath(rel)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"message": confError(err)})
		return
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		ctx.JSON(http.StatusOK, gin.H{"message": "只能保存文件"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": services.WriteConfigChecked(abs, []byte(ctx.PostForm("content")))})
}

// CheckNginxConf runs nginx -t and shows what nginx says.
func CheckNginxConf(ctx *gin.Context) {
	ok, output := services.CheckNginxConfig()
	ctx.JSON(http.StatusOK, gin.H{"ok": ok, "output": output})
}

// GetNginxConf opens the main configuration file, the one nginx -V names, in
// the editor. It is always in the configuration directory: that directory is
// defined as the one holding it.
func GetNginxConf(ctx *gin.Context) {
	conf := config.ReadNginxCompileInfo().NginxConfPath
	if conf == "" {
		ctx.String(http.StatusNotFound, "没有找到 Nginx，请先安装")
		return
	}
	ctx.Redirect(http.StatusFound, "/admin/nginx/file?"+url.Values{"path": {filepath.Base(conf)}}.Encode())
}
