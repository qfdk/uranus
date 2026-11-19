package controllers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// The log page reads the files nginx writes to, and nothing else.
func TestTailNginxLog(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "blog.access.log")
	mustWrite(t, logFile, "1.2.3.4 - - \"GET / HTTP/1.1\" 200 12\n5.6.7.8 - - \"GET /x HTTP/1.1\" 404 0\n")
	site := filepath.Join(sitesDir, "blog.conf")
	mustWrite(t, site, "server {\n    access_log "+logFile+";\n}\n")
	t.Cleanup(func() { os.Remove(site) })

	engine := gin.New()
	engine.GET("/logs/tail", TailNginxLog)
	tail := func(file string) (int, []string) {
		w := get(engine, "/logs/tail?"+url.Values{"file": {file}, "lines": {"1"}}.Encode())
		var body struct{ Lines []string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body.Lines
	}
	if code, lines := tail(logFile); code != http.StatusOK || len(lines) != 1 || lines[0] != "5.6.7.8 - - \"GET /x HTTP/1.1\" 404 0" {
		t.Fatalf("log file: %d %q", code, lines)
	}
	for _, file := range []string{"/etc/passwd", filepath.Join(filepath.Dir(nginxConfPath), "config.toml"), ""} {
		if code, lines := tail(file); code != http.StatusNotFound || len(lines) != 0 {
			t.Fatalf("%q readable: %d %q", file, code, lines)
		}
	}
}
