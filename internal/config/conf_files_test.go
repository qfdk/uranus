package config

import (
	"errors"
	"os"
	"path/filepath"
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
