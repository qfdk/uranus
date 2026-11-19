package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uranus/internal/models"

	"github.com/gin-gonic/gin"
)

func filesEngine() *gin.Engine {
	engine := gin.New()
	engine.GET("/nginx/config", GetNginxConf)
	engine.GET("/nginx/file", EditConfFile)
	engine.POST("/nginx/file/save", SaveConfFile)
	return engine
}

func saveConf(engine *gin.Engine, rel, content string) string {
	form := url.Values{"path": {rel}, "content": {content}}
	req := httptest.NewRequest(http.MethodPost, "/nginx/file/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var body struct{ Message string }
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Message
}

// The editor of nginx.conf is the file editor on the file nginx -V names,
// whatever it is called.
func TestNginxConfOpensTheMainConfigFile(t *testing.T) {
	w := get(filesEngine(), "/nginx/config")
	if loc := w.Header().Get("Location"); w.Code != http.StatusFound || loc != "/admin/nginx/file?path=nginx.conf" {
		t.Fatalf("redirect: %d %q", w.Code, loc)
	}
}

// Files of the configuration directory are saved like nginx.conf: checked,
// and their previous content kept. Nothing outside the directory is reachable.
func TestSaveConfFile(t *testing.T) {
	engine := filesEngine()
	confDir := filepath.Dir(nginxConfPath)
	snippet := filepath.Join(confDir, "snippets", "ssl.conf")
	mustWrite(t, snippet, "ssl_protocols TLSv1.2;\n")
	outside := filepath.Join(t.TempDir(), "outside.conf")
	mustWrite(t, outside, "untouched\n")
	if err := os.Symlink(outside, filepath.Join(confDir, "snippets", "escape.conf")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(confDir, "snippets")) })

	if got := saveConf(engine, "snippets/ssl.conf", "ssl_protocols TLSv1.3;\n"); got != "OK" {
		t.Fatalf("save: %q", got)
	}
	if content, _ := os.ReadFile(snippet); string(content) != "ssl_protocols TLSv1.3;\n" {
		t.Fatalf("not saved: %q", content)
	}
	if versions := models.ConfigVersions(snippet); len(versions) != 1 || versions[0].Content != "ssl_protocols TLSv1.2;\n" {
		t.Fatalf("previous content not kept: %+v", versions)
	}

	for _, rel := range []string{"snippets/escape.conf", "../../" + outside, "snippets"} {
		if got := saveConf(engine, rel, "pwned\n"); got == "OK" {
			t.Fatalf("saved %q", rel)
		}
	}
	if content, _ := os.ReadFile(outside); string(content) != "untouched\n" {
		t.Fatalf("file outside the configuration directory changed: %q", content)
	}

	// Directories open in the file list; links leading out are refused there.
	if loc := get(engine, "/nginx/file?path=snippets").Header().Get("Location"); loc != "/admin/nginx/files?dir=snippets" {
		t.Fatalf("directory: %q", loc)
	}
	loc := get(engine, "/nginx/file?path=snippets/escape.conf").Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/nginx/files?") || !strings.Contains(loc, "dir=snippets") || !strings.Contains(loc, "error=") {
		t.Fatalf("outside link: %q", loc)
	}
}
