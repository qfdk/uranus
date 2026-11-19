package controllers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func postDelete(path string) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.POST("/sites/delete", DeleteSiteConf)
	req := httptest.NewRequest(http.MethodPost, "/sites/delete", strings.NewReader(url.Values{"path": {path}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Deleting a site removes its configuration and certificate, and the page is
// told how the reload went: if it failed, nginx still serves the site.
func TestDeleteSiteConf(t *testing.T) {
	conf := filepath.Join(sitesDir, "blog.example.com.conf")
	cert := filepath.Join(sslDir, "blog.example.com", "cert.pem")
	other := filepath.Join(sslDir, "shop.example.com", "cert.pem")
	mustWrite(t, conf, "server { listen 80; }")
	mustWrite(t, cert, "cert")
	mustWrite(t, other, "cert")

	w := postDelete(conf)
	if w.Code != http.StatusFound {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	location, _ := url.Parse(w.Header().Get("Location"))
	if location.Path != "/admin/sites" || location.Query().Get("deleted") != "blog.example.com.conf" ||
		location.Query().Get("message") != "OK" {
		t.Fatalf("redirect = %q", w.Header().Get("Location"))
	}
	for _, gone := range []string{conf, filepath.Dir(cert)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", gone)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another site's certificate was removed: %v", err)
	}
}

// Only a site listed on the sites page can be deleted: a path from the request
// must not reach any other file.
func TestDeleteSiteConfRejectsOtherFiles(t *testing.T) {
	cert := filepath.Join(sslDir, "keep.example.com", "cert.pem")
	mustWrite(t, cert, "cert")

	for _, path := range []string{"", cert, filepath.Join(sitesDir, "..", "config.toml"), "blog.example.com.conf", nginxConfPath} {
		if w := postDelete(path); w.Code != http.StatusBadRequest {
			t.Fatalf("%q: %d %s", path, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(cert); err != nil {
		t.Fatalf("certificate removed: %v", err)
	}
}

// The configuration name becomes a file name in the sites directory; a name
// with a path in it must not write anywhere else.
func TestSaveSiteConfRejectsPathsInName(t *testing.T) {
	engine := gin.New()
	engine.POST("/sites/save", SaveSiteConf)
	outside := filepath.Join("..", "escape.conf")
	t.Cleanup(func() { os.Remove(outside) })

	for _, name := range []string{"../escape", "a/b", " ", ".hidden"} {
		form := url.Values{"filename": {name}, "content": {"server { listen 80; }"}}
		req := httptest.NewRequest(http.MethodPost, "/sites/save", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if !strings.Contains(w.Body.String(), "配置名称") {
			t.Fatalf("name %q accepted: %s", name, w.Body.String())
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a configuration was written outside the sites directory")
	}
}
