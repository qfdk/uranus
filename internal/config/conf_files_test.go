package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file pages reach nginx's own directory only: no "..", and no symbolic
// link that leads elsewhere, as one in sites-enabled could.
func TestConfDirPath(t *testing.T) {
	dir := t.TempDir()
	useFakeNginx(t, dir, filepath.Join(dir, "nginx.conf"))
	outside := t.TempDir()
	for _, f := range []string{"nginx.conf", "conf.d/blog.conf", "sites-available/default"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte("# "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "sites-enabled"), 0o755)
	os.Symlink("../sites-available/default", filepath.Join(dir, "sites-enabled", "default"))
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "sites-enabled", "escape"))
	os.Symlink(outside, filepath.Join(dir, "elsewhere"))

	for rel, want := range map[string]string{
		"":                      dir,
		"nginx.conf":            filepath.Join(dir, "nginx.conf"),
		"conf.d/blog.conf":      filepath.Join(dir, "conf.d", "blog.conf"),
		"/conf.d/blog.conf":     filepath.Join(dir, "conf.d", "blog.conf"),
		"sites-enabled/default": filepath.Join(dir, "sites-enabled", "default"),
		// .. is resolved against the root, so it can't climb above it
		"../../conf.d/blog.conf": filepath.Join(dir, "conf.d", "blog.conf"),
	} {
		if got, err := ConfDirPath(rel); err != nil || got != want {
			t.Errorf("ConfDirPath(%q) = %q, %v; want %q", rel, got, err, want)
		}
	}
	for _, rel := range []string{"sites-enabled/escape", "elsewhere", "elsewhere/secret"} {
		if got, err := ConfDirPath(rel); !errors.Is(err, ErrOutsideConfDir) {
			t.Errorf("ConfDirPath(%q) = %q, %v; want ErrOutsideConfDir", rel, got, err)
		}
	}
	if _, err := ConfDirPath("missing.conf"); err == nil {
		t.Error("a missing file was accepted")
	}

	files, err := ListConfDir("")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	// Directories first (the link to a directory counts as one), then files.
	want := []string{"conf.d", "elsewhere", "sites-available", "sites-enabled", "nginx.conf"}
	if len(names) != len(want) {
		t.Fatalf("listing: %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("listing: %q, want %q", names, want)
		}
	}
	enabled, _ := ListConfDir("sites-enabled")
	if len(enabled) != 2 || enabled[0].Link != "../sites-available/default" || enabled[0].Rel != "sites-enabled/default" {
		t.Fatalf("sites-enabled: %+v", enabled)
	}
}

func TestLogFiles(t *testing.T) {
	prefix := t.TempDir()
	for _, f := range []string{"logs/error.log", "logs/access.log", "var/blog.access.log", "var/quoted log.log", "var/compiled-http.log"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(prefix, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(prefix, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dump := "# configuration file " + prefix + "/nginx.conf:\n" +
		"error_log logs/error.log warn;\n" +
		"http {\n" +
		"    # access_log " + prefix + "/var/commented.log;\n" +
		"    access_log logs/access.log main;\n" +
		"    access_log syslog:server=127.0.0.1 combined;\n" +
		"    server {\n" +
		"        access_log " + prefix + "/var/blog.access.log;\n" +
		"        access_log \"" + prefix + "/var/quoted log.log\";\n" +
		"        access_log /var/log/nginx/$host.log;\n" +
		"        error_log " + prefix + "/var/missing.log;\n" +
		"        add_header X-Note access_log;\n" +
		"        access_log off;\n" +
		"    }\n" +
		"}\n"
	info := NginxCompileInfo{Prefix: prefix, Params: []string{
		"error-log-path=logs/error.log", "http-log-path=" + prefix + "/var/compiled-http.log",
	}}
	got := LogFiles(dump, info)
	want := []string{
		filepath.Join(prefix, "logs/error.log"), filepath.Join(prefix, "logs/access.log"),
		filepath.Join(prefix, "var/blog.access.log"), filepath.Join(prefix, "var/quoted log.log"),
		filepath.Join(prefix, "var/compiled-http.log"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("log files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
