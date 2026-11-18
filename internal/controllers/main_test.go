package controllers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"uranus/internal/config"

	"github.com/gin-gonic/gin"
)

var (
	nginxConfPath string
	// sitesDir and sslDir are the vhost and certificate directories of the test config
	sitesDir, sslDir string
	testAppConfig    string
)

// TestMain puts a fake nginx on PATH whose `nginx -V` points the main
// configuration file into a temporary directory.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "uranus-controllers-test")
	if err != nil {
		panic(err)
	}
	dir, _ = filepath.EvalSymlinks(dir)
	nginxConfPath = filepath.Join(dir, "nginx.conf")
	sitesDir = filepath.Join(dir, "sites")
	sslDir = filepath.Join(dir, "ssl")
	testAppConfig = "uuid = \"test-uuid\"\nip = \"127.0.0.1\"\ntoken = \"test-token\"\n" +
		"sessionsecret = \"test-secret\"\ncontrolcenter = \"\"\n" +
		"installpath = \"" + dir + "\"\nvhostpath = \"" + sitesDir + "\"\nsslpath = \"" + sslDir + "\"\n"

	bin := filepath.Join(dir, "bin")
	script := "#!/bin/sh\n" +
		"echo 'nginx version: nginx/1.27.0' >&2\n" +
		"echo 'built by gcc 12' >&2\n" +
		"echo 'configure arguments: --conf-path=" + nginxConfPath + " --pid-path=" + dir + "/nginx.pid' >&2\n"
	if err := os.MkdirAll(bin, 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "nginx"), []byte(script), 0o755); err != nil {
		panic(err)
	}
	os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(testAppConfig), 0o600); err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	config.GetAppConfig()
	// Shells started by the terminal tests must not read or write the user's home.
	os.Setenv("HOME", dir)
	os.Setenv("BASH_SILENCE_DEPRECATION_WARNING", "1")
	os.Unsetenv("TERM_PROGRAM")

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func get(engine *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}
