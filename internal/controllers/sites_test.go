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
