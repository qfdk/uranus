package config

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrOutsideConfDir is returned for a path that leaves the nginx configuration
// directory, directly or through a symbolic link.
var ErrOutsideConfDir = errors.New("不在 Nginx 配置目录里")

// ConfFile is an entry of the nginx configuration directory.
type ConfFile struct {
	Name string
	// Rel is the path relative to the configuration directory, with slashes.
	Rel   string
	IsDir bool
	// Link is the target of a symbolic link.
	Link    string
	Size    int64
	ModTime time.Time
}

// ConfDirPath returns the absolute path of rel, a path relative to the nginx
// configuration directory. The path must exist, and neither rel nor a
// symbolic link on the way may lead out of the directory: the file pages can
// only reach nginx's own files.
func ConfDirPath(rel string) (string, error) {
	root := NginxConfDir()
	abs := filepath.Join(root, filepath.FromSlash(path.Clean("/"+rel)))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", ErrOutsideConfDir
	}
	return abs, nil
}

// ConfRel returns rel cleaned the way ConfDirPath reads it: "" for the
// configuration directory itself, otherwise like "conf.d/blog.conf".
func ConfRel(rel string) string {
	return strings.TrimPrefix(path.Clean("/"+rel), "/")
}

// ListConfDir lists a directory inside the nginx configuration directory:
// directories first, then files, each sorted by name.
func ListConfDir(rel string) ([]ConfFile, error) {
	dir, err := ConfDirPath(rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	base := ConfRel(rel)
	var files []ConfFile
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		info, err := os.Stat(full) // follows links: a link to a directory is listed as one
		if err != nil {
			info, err = os.Lstat(full)
			if err != nil {
				continue
			}
		}
		file := ConfFile{
			Name:    entry.Name(),
			Rel:     path.Join(base, entry.Name()),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if entry.Type()&os.ModeSymlink != 0 {
			file.Link, _ = os.Readlink(full)
		}
		files = append(files, file)
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}

// LogFiles returns the log files nginx writes: those its configuration names
// with access_log and error_log (dump is the output of nginx -T), then the
// compiled-in ones. Relative paths are relative to the prefix, as in nginx.
// syslog, stderr, buffers in memory and paths with variables are not files
// that can be read here, and missing files are left out.
func LogFiles(dump string, info NginxCompileInfo) []string {
	var files []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || p == "off" || p == "stderr" || strings.Contains(p, "$") ||
			strings.HasPrefix(p, "syslog:") || strings.HasPrefix(p, "memory:") {
			return
		}
		p = filepath.Clean(underPrefix(info.Prefix, p, ""))
		if seen[p] {
			return
		}
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			return
		}
		seen[p] = true
		files = append(files, p)
	}

	tokens, _ := tokenizeConf(dump)
	statementStart := true
	for i, t := range tokens {
		if statementStart && !t.quoted && (t.text == "access_log" || t.text == "error_log") && i+1 < len(tokens) {
			add(tokens[i+1].text)
		}
		// the next word starts a directive after ; { or }
		statementStart = !t.quoted && (t.text == ";" || t.text == "{" || t.text == "}")
	}
	// Params are the configure arguments without their leading --
	for _, param := range info.Params {
		name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
		if ok && (name == "error-log-path" || name == "http-log-path") {
			add(strings.TrimSpace(value))
		}
	}
	return files
}
