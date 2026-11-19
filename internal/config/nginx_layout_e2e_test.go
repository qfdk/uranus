package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRealNginxLayout checks the detection against a real nginx installation;
// scripts/test-nginx-layouts.sh runs it in throwaway containers, one per
// distribution. WANT_SITES and WANT_INCLUDED give the expected sites
// directory and whether nginx.conf includes it.
//
// With URANUS_NGINX_E2E_WRITE=1 it also changes the installation: it adds the
// include when needed, saves a site in the sites directory, checks the
// configuration and starts nginx. Never set it on a machine you care about.
func TestRealNginxLayout(t *testing.T) {
	if os.Getenv("URANUS_NGINX_E2E") == "" {
		t.Skip("needs an nginx installation, see scripts/test-nginx-layouts.sh")
	}
	info := ReadNginxCompileInfo()
	if info.NginxConfPath == "" {
		t.Fatal("nginx not found")
	}
	sites := Sites()
	t.Logf("%s: binary %s, main config %s, pid file %s, sites %s (included: %v), certificates %s",
		info.Version, info.NginxExec, info.NginxConfPath, NginxPidFile(), sites.Dir, sites.Included, SSLDir())
	if want := os.Getenv("WANT_SITES"); want != "" && sites.Dir != want {
		t.Errorf("sites directory %s, want %s", sites.Dir, want)
	}
	if want := os.Getenv("WANT_INCLUDED"); want != "" && strconv.FormatBool(sites.Included) != want {
		t.Errorf("included: %v, want %s", sites.Included, want)
	}
	if os.Getenv("URANUS_NGINX_E2E_WRITE") == "" || t.Failed() {
		return
	}

	nginx := func(args ...string) {
		t.Helper()
		out, err := exec.Command(info.NginxExec, append([]string{"-c", info.NginxConfPath}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("nginx %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	if !sites.Included {
		src, err := os.ReadFile(info.NginxConfPath)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := AddSitesInclude(string(src), sites.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(info.NginxConfPath, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
		if !Sites().Included {
			t.Fatal("the include that was added is not detected")
		}
	}

	// A site saved in the sites directory must be valid where nginx includes it.
	site := "server {\n    listen 8081;\n    server_name e2e.test;\n    location / { return 200 'ok'; }\n}\n"
	if err := os.MkdirAll(sites.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sites.Dir, "uranus-e2e.conf"), []byte(site), 0o644); err != nil {
		t.Fatal(err)
	}
	nginx("-t")

	// The master writes its pid where NginxPidFile says.
	nginx()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(NginxPidFile())
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid > 0 {
			if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
				t.Fatalf("pid file %s names %d, which does not run", NginxPidFile(), pid)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pid file at %s after starting nginx", NginxPidFile())
		}
		time.Sleep(100 * time.Millisecond)
	}
	nginx("-s", "quit")
}
