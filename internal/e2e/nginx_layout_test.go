// Package e2e checks Uranus against real nginx installations, see
// scripts/test-nginx-layouts.sh.
package e2e

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"uranus/internal/config"
	"uranus/internal/services"
)

// TestMain runs the tests next to a config.toml, as Uranus runs next to its
// own; without one Uranus would create it, looking up the public IP online.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uranus-e2e")
	if err != nil {
		panic(err)
	}
	cfg := "uuid = \"e2e\"\nip = \"127.0.0.1\"\ntoken = \"e2e\"\nsessionsecret = \"e2e\"\ncontrolcenter = \"\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestRealNginxLayout checks what Uranus finds on a real nginx installation;
// scripts/test-nginx-layouts.sh runs it in throwaway containers, one per
// installation. WANT_SITES and WANT_INCLUDED give the expected sites
// directory and whether nginx.conf includes it.
//
// With URANUS_NGINX_E2E_WRITE=1 it then does what the web interface does: it
// adds the include when needed, saves a site, and starts, reloads, restarts
// and stops nginx. Never set it on a machine you care about.
func TestRealNginxLayout(t *testing.T) {
	if os.Getenv("URANUS_NGINX_E2E") == "" {
		t.Skip("needs an nginx installation, see scripts/test-nginx-layouts.sh")
	}
	info := config.ReadNginxCompileInfo()
	if info.NginxConfPath == "" {
		t.Fatal("nginx not found")
	}
	sites := config.Sites()
	t.Logf("%s: binary %s, main config %s, pid file %s, sites %s (included: %v), certificates %s, control: %s",
		info.Version, info.NginxExec, info.NginxConfPath, config.NginxPidFile(),
		sites.Dir, sites.Included, config.SSLDir(), services.NginxManager())
	if want := os.Getenv("WANT_SITES"); want != "" && sites.Dir != want {
		t.Errorf("sites directory %s, want %s", sites.Dir, want)
	}
	if want := os.Getenv("WANT_INCLUDED"); want != "" && strconv.FormatBool(sites.Included) != want {
		t.Errorf("included: %v, want %s", sites.Included, want)
	}
	if os.Getenv("URANUS_NGINX_E2E_WRITE") == "" || t.Failed() {
		return
	}

	// The button the sites page shows when nginx.conf does not include the directory.
	if got := services.IncludeSitesDir(); got != "OK" {
		t.Fatalf("include the sites directory: %s", got)
	}
	if !config.Sites().Included {
		t.Fatal("the include that was added is not detected")
	}

	// Saving a site, as the site editor does.
	if err := os.MkdirAll(sites.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	site := "server {\n    listen 8081;\n    location / { return 200 'uranus e2e'; }\n}\n"
	if got := services.WriteConfigChecked(filepath.Join(sites.Dir, "uranus-e2e.conf"), []byte(site)); got != "OK" {
		t.Fatalf("save a site: %s", got)
	}

	if got := services.StartNginx(); got != "OK" {
		t.Fatalf("start: %s", got)
	}
	pid := services.NginxStatus()
	if pid == "KO" {
		t.Fatalf("nginx is not running after the start, no pid in %s", config.NginxPidFile())
	}
	assertServesSite(t)

	if got := services.ReloadNginx(); got != "OK" {
		t.Fatalf("reload: %s", got)
	}
	if got := services.RestartNginx(); got != "OK" {
		t.Fatalf("restart: %s", got)
	}
	if again := services.NginxStatus(); again == "KO" || again == pid {
		t.Fatalf("nginx runs as %s after the restart, as %s before", again, pid)
	}
	assertServesSite(t)

	if got := services.StopNginx(); got != "OK" {
		t.Fatalf("stop: %s", got)
	}
	if got := services.NginxStatus(); got != "KO" {
		t.Fatalf("nginx still runs as %s after the stop", got)
	}
}

// assertServesSite checks that nginx serves the site saved in the sites directory.
func assertServesSite(t *testing.T) {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8081/")
	if err != nil {
		t.Fatalf("the saved site: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "uranus e2e" {
		t.Fatalf("the saved site answered %d %q", resp.StatusCode, body)
	}
}
