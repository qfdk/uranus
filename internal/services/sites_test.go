package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uranus/internal/config"
	"uranus/internal/models"
)

// siteLayout writes a main configuration that loads conf.d/*.conf and
// Debian's sites-enabled/*, with one site of each kind.
func siteLayout(t *testing.T) (file, link, target string) {
	t.Helper()
	conf := filepath.Join(fakeDir, "custom-main.conf")
	main := "events {}\nhttp {\n    include conf.d/*.conf;\n    include sites-enabled/*;\n}\n"
	dirs := []string{"conf.d", "sites-enabled", "sites-available", "disabled-sites"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(fakeDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		os.Remove(conf)
		for _, d := range dirs {
			os.RemoveAll(filepath.Join(fakeDir, d))
		}
		for _, s := range models.DisabledSites() {
			_ = s.Forget()
		}
	})
	if err := os.WriteFile(conf, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(fakeDir, "conf.d", "blog.conf")
	if err := os.WriteFile(file, []byte("server { listen 80; server_name blog; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(fakeDir, "sites-available", "default")
	if err := os.WriteFile(target, []byte("server { listen 80 default_server; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(fakeDir, "sites-enabled", "default")
	if err := os.Symlink("../sites-available/default", link); err != nil {
		t.Fatal(err)
	}
	return file, link, target
}

func siteNamed(t *testing.T, path string) config.SiteFile {
	t.Helper()
	site, ok := config.FindSiteFile(path)
	if !ok {
		t.Fatalf("%s is not a site; sites: %+v", path, config.SiteFiles())
	}
	return site
}

func disabledRecord(t *testing.T, path string) models.DisabledSite {
	t.Helper()
	for _, s := range models.DisabledSites() {
		if s.Path == path {
			return s
		}
	}
	t.Fatalf("no disabled record for %s", path)
	return models.DisabledSite{}
}

// A disabled site leaves the places nginx loads, keeps its configuration and
// comes back unchanged.
func TestDisableAndEnableSite(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	file, link, target := siteLayout(t)

	if got := DisableSite(siteNamed(t, file)); got != "OK" {
		t.Fatalf("disable file: %q", got)
	}
	assertCalls(t, "nginx -t", "nginx -t", "nginx -s reload")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("disabled file still where nginx loads it")
	}
	stored := disabledRecord(t, file)
	if content, err := DisabledSiteConfig(stored); err != nil || !strings.Contains(string(content), "server_name blog") {
		t.Fatalf("stored configuration: %q %v", content, err)
	}

	if got := DisableSite(siteNamed(t, link)); got != "OK" {
		t.Fatalf("disable link: %q", got)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("link still in sites-enabled")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("link target removed: %v", err)
	}
	if n := len(config.SiteFiles()); n != 0 {
		t.Fatalf("disabled sites still listed: %+v", config.SiteFiles())
	}
	takeCalls(t)

	for _, path := range []string{file, link} {
		if got := EnableSite(disabledRecord(t, path).ID); got != "OK" {
			t.Fatalf("enable %s: %q", path, got)
		}
	}
	assertCalls(t, "nginx -t", "nginx -t", "nginx -s reload", "nginx -t", "nginx -t", "nginx -s reload")
	if content, _ := os.ReadFile(file); !strings.Contains(string(content), "server_name blog") {
		t.Fatalf("file not back: %q", content)
	}
	if got, err := os.Readlink(link); err != nil || got != "../sites-available/default" {
		t.Fatalf("link not restored as it was: %q %v", got, err)
	}
	if left := models.DisabledSites(); len(left) != 0 {
		t.Fatalf("records left: %+v", left)
	}
}

// nginx may refuse the configuration without a site (another file uses its
// upstream) or with it: nothing changes then.
func TestSiteStaysWhenNginxRefuses(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	file, _, _ := siteLayout(t)

	t.Setenv("FAKE_NGINX_TEST_FAIL", "nginx: [emerg] host not found in upstream \"blog\"")
	if got := DisableSite(siteNamed(t, file)); !strings.Contains(got, "host not found") {
		t.Fatalf("refused disable: %q", got)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("site not restored: %v", err)
	}
	if left := models.DisabledSites(); len(left) != 0 {
		t.Fatalf("refused disable recorded: %+v", left)
	}

	t.Setenv("FAKE_NGINX_TEST_FAIL", "")
	if got := DisableSite(siteNamed(t, file)); got != "OK" {
		t.Fatalf("disable: %q", got)
	}
	record := disabledRecord(t, file)
	t.Setenv("FAKE_NGINX_TEST_FAIL", "nginx: [emerg] duplicate listen options")
	if got := EnableSite(record.ID); !strings.Contains(got, "duplicate listen") {
		t.Fatalf("refused enable: %q", got)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("refused site left where nginx loads it")
	}
	if _, err := os.Stat(record.Stored); err != nil {
		t.Fatalf("stored file lost: %v", err)
	}
	disabledRecord(t, file)
}

// A new file at the old place is not overwritten.
func TestEnableSiteKeepsNewFile(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	file, _, _ := siteLayout(t)
	if got := DisableSite(siteNamed(t, file)); got != "OK" {
		t.Fatalf("disable: %q", got)
	}
	if err := os.WriteFile(file, []byte("server { listen 81; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := EnableSite(disabledRecord(t, file).ID); !strings.Contains(got, "已经有文件") {
		t.Fatalf("enable over a new file: %q", got)
	}
	if content, _ := os.ReadFile(file); string(content) != "server { listen 81; }\n" {
		t.Fatalf("new file overwritten: %q", content)
	}
}

func TestDeleteDisabledSite(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	file, link, target := siteLayout(t)
	for _, path := range []string{file, link} {
		if got := DisableSite(siteNamed(t, path)); got != "OK" {
			t.Fatalf("disable %s: %q", path, got)
		}
	}
	stored := disabledRecord(t, file).Stored
	for _, path := range []string{file, link} {
		if err := DeleteDisabledSite(disabledRecord(t, path)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Fatal("stored configuration not deleted")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("the target of a deleted link must stay: %v", err)
	}
	if left := models.DisabledSites(); len(left) != 0 {
		t.Fatalf("records left: %+v", left)
	}
}

// A disabled site still needs its certificate to be enabled again, but it is
// not renewed while nginx does not serve it.
func TestDisabledSiteKeepsCertificateWithoutRenewal(t *testing.T) {
	simulateHost(t, "linux", false)
	writePidFile(t, os.Getpid())
	file, _, _ := siteLayout(t)
	cert := filepath.Join(fakeDir, "ssl", "blog", "fullchain.cer")
	if err := os.WriteFile(file, []byte("server {\n    listen 443 ssl;\n    ssl_certificate "+cert+";\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DisableSite(siteNamed(t, file)); got != "OK" {
		t.Fatalf("disable: %q", got)
	}
	if got := CertificateUsers("blog"); len(got) != 1 || !strings.Contains(got[0], file) || !strings.Contains(got[0], "已停用") {
		t.Fatalf("the disabled site must count as a user: %q", got)
	}

	now := time.Now()
	soon := now.Add(10 * 24 * time.Hour)
	certs := []models.Cert{
		{FileName: "blog", Domains: "blog.example", NotAfter: soon},
		{FileName: "shop", Domains: "shop.example", NotAfter: soon},
		{FileName: "docs", Domains: "docs.example", NotAfter: now.Add(60 * 24 * time.Hour)},
		{FileName: "new", Domains: "new.example"},
	}
	due := certsToRenew(certs, now)
	if len(due) != 1 || due[0].FileName != "shop" {
		t.Fatalf("certificates to renew: %+v", due)
	}
}
