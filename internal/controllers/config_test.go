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

// config.toml holds the admin password and the agent token: the saved file and
// its backups must stay readable by their owner only.
func TestSaveConfigKeepsCredentialsPrivate(t *testing.T) {
	engine := gin.New()
	engine.POST("/config/save", SaveConfig)

	form := url.Values{"content": {testAppConfig + "email = \"ops@example.com\"\n"}}
	req := httptest.NewRequest(http.MethodPost, "/config/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
	}

	backups, _ := filepath.Glob("config.toml.backup.*")
	if len(backups) == 0 {
		t.Fatal("no backup written")
	}
	for _, f := range append(backups, "config.toml") {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s permissions = %o, want 600", f, perm)
		}
	}
}
