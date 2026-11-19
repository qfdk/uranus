package services

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Private keys used to be written world-readable (0644), letting any local
// user, such as the web server's own account, read the site's TLS key.
func TestSaveCertificateKeepsPrivateKeyPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "example")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A key left behind by an older version is tightened on renewal.
	if err := os.WriteFile(filepath.Join(dir, "private.key"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveCertificate(dir, []byte("chain"), []byte("key"), []string{"a.example", "b.example"}); err != nil {
		t.Fatalf("saveCertificate: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "private.key"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("private.key permissions = %o, want 600", perm)
	}
	for name, want := range map[string]string{"fullchain.cer": "chain", "private.key": "key", "domains": "a.example,b.example"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q (%v), want %q", name, got, err, want)
		}
	}
}

func TestSaveCertificateReportsWriteErrors(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveCertificate(filepath.Join(notADir, "example"), []byte("chain"), []byte("key"), nil); err == nil {
		t.Fatal("expected an error when the certificate directory can't be created")
	}
}

// A certificate is in use while a file nginx loads names it: deleting it would
// make the next reload fail. nginx -T lists every file nginx loads; with a
// broken configuration it lists nothing and the sites directory is searched.
func TestCertificateUsers(t *testing.T) {
	simulateHost(t, "linux", false)
	sitesDir := filepath.Join(fakeDir, "conf.d")
	site := filepath.Join(sitesDir, "blog.example.com.conf")
	elsewhere := filepath.Join(fakeDir, "elsewhere.conf")
	uses := "server {\n    listen 443 ssl;\n    ssl_certificate " +
		filepath.Join(fakeDir, "ssl", "blog.example.com", "fullchain.cer") + ";\n}\n"
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{site, elsewhere} {
		if err := os.WriteFile(path, []byte(uses), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(sitesDir); os.Remove(elsewhere) })

	if got := CertificateUsers("blog.example.com"); !slices.Equal(got, []string{elsewhere, site}) {
		t.Fatalf("users: %q", got)
	}
	if got := CertificateUsers("shop.example.com"); len(got) != 0 {
		t.Fatalf("unused certificate used by %q", got)
	}
	t.Setenv("FAKE_NGINX_TEST_FAIL", "nginx: [emerg] unexpected end of file")
	if got := CertificateUsers("blog.example.com"); !slices.Equal(got, []string{site}) {
		t.Fatalf("users with a broken configuration: %q", got)
	}
}
