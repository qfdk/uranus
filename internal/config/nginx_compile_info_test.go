package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNginxInfoPaths(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		conf     string
		pid      string
		version  string
		compiler string
	}{
		{
			name: "Homebrew on macOS",
			output: "nginx version: nginx/1.27.1\nbuilt by clang 15.0.0 (clang-1500.3.9.4)\n" +
				"built with OpenSSL 3.3.1 4 Jun 2024\nTLS SNI support enabled\n" +
				"configure arguments: --prefix=/opt/homebrew/Cellar/nginx/1.27.1 --sbin-path=/opt/homebrew/Cellar/nginx/1.27.1/bin/nginx " +
				"--with-cc-opt='-I/opt/homebrew/opt/pcre2/include' --conf-path=/opt/homebrew/etc/nginx/nginx.conf " +
				"--pid-path=/opt/homebrew/var/run/nginx.pid --with-http_ssl_module\n",
			conf:     "/opt/homebrew/etc/nginx/nginx.conf",
			pid:      "/opt/homebrew/var/run/nginx.pid",
			version:  "nginx/1.27.1",
			compiler: "clang 15.0.0 (clang-1500.3.9.4)",
		},
		{
			name: "Ubuntu package",
			output: "nginx version: nginx/1.24.0 (Ubuntu)\nbuilt with OpenSSL 3.0.13 30 Jan 2024\nTLS SNI support enabled\n" +
				"configure arguments: --with-cc-opt='-g -O2 -ffile-prefix-map=/build/nginx-1.24.0=. -D_FORTIFY_SOURCE=3' " +
				"--prefix=/usr/share/nginx --conf-path=/etc/nginx/nginx.conf --http-log-path=/var/log/nginx/access.log " +
				"--error-log-path=stderr --lock-path=/var/lock/nginx.lock --pid-path=/run/nginx.pid\n",
			conf:     "/etc/nginx/nginx.conf",
			pid:      "/run/nginx.pid",
			version:  "nginx/1.24.0 (Ubuntu)",
			compiler: "Non-compiled version",
		},
		{
			name: "Alpine package (Docker image)",
			output: "nginx version: nginx/1.26.2\nbuilt by gcc 13.2.1 20240309 (Alpine 13.2.1_git20240309)\n" +
				"built with OpenSSL 3.3.0 9 Apr 2024\nTLS SNI support enabled\n" +
				"configure arguments: --prefix=/var/lib/nginx --sbin-path=/usr/sbin/nginx --conf-path=/etc/nginx/nginx.conf " +
				"--pid-path=/run/nginx/nginx.pid --lock-path=/run/nginx/nginx.lock\n",
			conf:    "/etc/nginx/nginx.conf",
			pid:     "/run/nginx/nginx.pid",
			version: "nginx/1.26.2",
		},
		{
			name:    "built from source with defaults",
			output:  "nginx version: nginx/1.26.2\nbuilt by gcc 12.2.0 (Debian 12.2.0-14)\nconfigure arguments: --with-http_ssl_module\n",
			conf:    "/usr/local/nginx/conf/nginx.conf",
			pid:     "/usr/local/nginx/logs/nginx.pid",
			version: "nginx/1.26.2",
		},
		{
			name: "OpenResty",
			output: "nginx version: openresty/1.25.3.1\nbuilt with OpenSSL 3.0.12 24 Oct 2023\nTLS SNI support enabled\n" +
				"configure arguments: --prefix=/usr/local/openresty/nginx --with-cc-opt=-O2 --with-http_ssl_module\n",
			conf:    "/usr/local/openresty/nginx/conf/nginx.conf",
			pid:     "/usr/local/openresty/nginx/logs/nginx.pid",
			version: "openresty/1.25.3.1",
		},
		{
			name:   "relative paths",
			output: "nginx version: nginx/1.26.2\nbuilt by gcc 12\nconfigure arguments: --prefix=/opt/ng --conf-path=conf/main.conf --pid-path=run/ng.pid\n",
			conf:   "/opt/ng/conf/main.conf",
			pid:    "/opt/ng/run/ng.pid",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := parseNginxInfo(c.output)
			if info.NginxConfPath != c.conf || info.NginxPidPath != c.pid {
				t.Fatalf("conf=%q pid=%q, want conf=%q pid=%q", info.NginxConfPath, info.NginxPidPath, c.conf, c.pid)
			}
			if c.version != "" && info.Version != c.version {
				t.Fatalf("version=%q, want %q", info.Version, c.version)
			}
			if c.compiler != "" && info.CompilerVersion != c.compiler {
				t.Fatalf("compiler=%q, want %q", info.CompilerVersion, c.compiler)
			}
		})
	}
}

// resetNginxDetection forgets cached detection results for the duration of a
// test and makes sure the test never triggers a real package install.
func resetNginxDetection(t *testing.T) {
	t.Helper()
	autoInstallOnce.Do(func() {})
	savedPaths := nginxSearchPaths
	nciMutex.Lock()
	savedInfo, savedInstalled := _nci, nciInstalled
	_nci, nciInstalled = nil, false
	nciMutex.Unlock()
	t.Cleanup(func() {
		nginxSearchPaths = savedPaths
		nciMutex.Lock()
		_nci, nciInstalled = savedInfo, savedInstalled
		nciMutex.Unlock()
	})
}

func writeExecutable(t *testing.T, path, script string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// nginx installed after uranus started (e.g. from the web terminal) must be
// noticed without restarting uranus.
func TestNginxInstalledLaterIsDetected(t *testing.T) {
	resetNginxDetection(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	nginxSearchPaths = nil

	if info := ReadNginxCompileInfo(); info.NginxConfPath != "" || !strings.HasPrefix(info.Version, "Not installed") {
		t.Fatalf("expected nginx to be missing, got %+v", info)
	}

	writeExecutable(t, filepath.Join(bin, "nginx"), "#!/bin/sh\n"+
		"echo 'nginx version: nginx/1.27.0' >&2\n"+
		"echo 'built by gcc 12' >&2\n"+
		"echo 'configure arguments: --conf-path=/srv/nginx/nginx.conf --pid-path=/srv/nginx/nginx.pid' >&2\n")

	info := ReadNginxCompileInfo()
	if info.Version != "nginx/1.27.0" || info.NginxConfPath != "/srv/nginx/nginx.conf" {
		t.Fatalf("newly installed nginx not detected: %+v", info)
	}
}

// Under launchd (macOS) or a minimal service PATH, nginx is not on PATH.
func TestNginxBinaryFallsBackToKnownLocations(t *testing.T) {
	resetNginxDetection(t)
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	installed := filepath.Join(dir, "homebrew", "bin", "nginx")
	writeExecutable(t, installed, "#!/bin/sh\n")
	nginxSearchPaths = []string{filepath.Join(dir, "missing", "nginx"), installed}

	if got := NginxBinary(); got != installed {
		t.Fatalf("NginxBinary() = %q, want %q", got, installed)
	}

	onPath := t.TempDir()
	writeExecutable(t, filepath.Join(onPath, "nginx"), "#!/bin/sh\n")
	t.Setenv("PATH", onPath)
	if got := NginxBinary(); got != filepath.Join(onPath, "nginx") {
		t.Fatalf("nginx on PATH should win, got %q", got)
	}
}
