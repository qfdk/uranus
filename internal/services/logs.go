package services

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"uranus/internal/config"
)

var (
	logFilesMu      sync.Mutex
	logFilesCache   []string
	logFilesExpires time.Time
)

// NginxLogFiles returns the log files nginx writes (see config.LogFiles). The
// list is kept for 10 seconds: the log page refreshes every few seconds.
func NginxLogFiles() []string {
	logFilesMu.Lock()
	defer logFilesMu.Unlock()
	if time.Now().Before(logFilesExpires) {
		return logFilesCache
	}

	var dump strings.Builder
	if out, err := nginxCommand("-T").Output(); err == nil {
		dump.Write(out)
	} else {
		// nginx -T prints nothing for a broken configuration: read the files Uranus knows
		info := config.ReadNginxCompileInfo()
		files := []string{info.NginxConfPath}
		for _, site := range config.SiteFiles() {
			files = append(files, site.Path)
		}
		for _, f := range files {
			if content, err := os.ReadFile(f); err == nil {
				fmt.Fprintf(&dump, "%s\n", content)
			}
		}
	}
	logFilesCache = config.LogFiles(dump.String(), *config.ReadNginxCompileInfo())
	logFilesExpires = time.Now().Add(10 * time.Second)
	return logFilesCache
}

// IsNginxLogFile reports whether path is one of NginxLogFiles: the log page
// reads those and nothing else.
func IsNginxLogFile(path string) bool {
	return slices.Contains(NginxLogFiles(), path)
}

// maxTailBytes is how much of the end of a log file TailFile reads.
const maxTailBytes = 512 << 10

// TailFile returns the last n lines of a file, read from at most its last
// maxTailBytes, with invalid UTF-8 replaced.
func TailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(info.Size()-maxTailBytes, 0)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		// the first line read is probably cut
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	text := strings.ToValidUTF8(strings.TrimRight(string(data), "\n"), "�")
	if text == "" {
		return []string{}, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
