package controllers

import (
	"html/template"
	"net/http"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// nginx.conf may be edited outside the editor (web terminal, ssh). The editor
// used to keep showing its first read, so saving silently reverted those edits.
func TestNginxEditorShowsCurrentFile(t *testing.T) {
	engine := gin.New()
	engine.SetHTMLTemplate(template.Must(template.New("nginxEdit.html").Parse("{{.content}}")))
	engine.GET("/nginx/config", GetNginxConf)

	for _, content := range []string{"worker_processes 1;", "worker_processes 4;"} {
		if err := os.WriteFile(nginxConfPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if w := get(engine, "/nginx/config"); w.Code != http.StatusOK || w.Body.String() != content {
			t.Fatalf("editor shows %q (%d), file contains %q", w.Body.String(), w.Code, content)
		}
	}
}
