package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var testConfigFile string

// TestMain starts from a config.toml as written by older versions: no session
// secret and world-readable. ip is set so loading never touches the network.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uranus-config-test")
	if err != nil {
		panic(err)
	}
	cfg := "uuid = \"test-uuid\"\nip = \"127.0.0.1\"\ntoken = \"old\"\n" +
		"email = \"old@example.com\"\npassword = \"pw\"\nusername = \"admin\"\n"
	testConfigFile = filepath.Join(dir, "config.toml")
	if err := os.WriteFile(testConfigFile, []byte(cfg), 0o644); err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestSessionSecretIsGeneratedAndPersisted(t *testing.T) {
	secret := GetAppConfig().SessionSecret
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(secret) {
		t.Fatalf("expected a 256-bit hex session secret, got %q", secret)
	}
	content, err := os.ReadFile(testConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`sessionsecret\s*=\s*['"]` + secret).Match(content) {
		t.Fatalf("session secret not saved to config.toml:\n%s", content)
	}
}

// config.toml holds the admin password, the agent token and the session secret.
func TestConfigFileIsOwnerOnly(t *testing.T) {
	GetAppConfig()
	info, err := os.Stat(testConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config.toml permissions = %o, want 600", perm)
	}
}

func TestGenerateSecureToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		token := GenerateSecureToken()
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token %q is not URL-safe base64: %v", token, err)
		}
		if len(raw) < 16 {
			t.Fatalf("token %q carries %d random bytes, want at least 16", token, len(raw))
		}
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true
	}
}

// Callers keep the *AppConfig they got; a reload must publish a new snapshot
// instead of rewriting the one they are reading.
func TestReloadDoesNotMutateHandedOutConfig(t *testing.T) {
	before := GetAppConfig()
	oldEmail := before.Email
	newEmail := fmt.Sprintf("reload-%d@example.com", time.Now().UnixNano())

	content, err := os.ReadFile(testConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	updated := regexp.MustCompile(`email\s*=\s*['"][^'"]*['"]`).ReplaceAll(content, []byte(`email = "`+newEmail+`"`))
	if err := os.WriteFile(testConfigFile, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for GetAppConfig().Email != newEmail {
		if time.Now().After(deadline) {
			t.Fatal("config change was not picked up")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if before.Email != oldEmail {
		t.Fatalf("reload rewrote a config snapshot already handed out: email is now %q", before.Email)
	}
}

// The config editor saves by renaming a temporary file over config.toml; hot
// reload has to keep working after the file has been replaced.
func TestReloadAfterFileReplacement(t *testing.T) {
	for i := 0; i < 2; i++ {
		email := fmt.Sprintf("replaced-%d-%d@example.com", i, time.Now().UnixNano())
		content, err := os.ReadFile(testConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		updated := regexp.MustCompile(`email\s*=\s*['"][^'"]*['"]`).ReplaceAll(content, []byte(`email = "`+email+`"`))
		tmp := testConfigFile + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, testConfigFile); err != nil {
			t.Fatal(err)
		}

		deadline := time.Now().Add(8 * time.Second)
		for GetAppConfig().Email != email {
			if time.Now().After(deadline) {
				t.Fatalf("replacement with email %q was not picked up (still %q)", email, GetAppConfig().Email)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// serveAnswer starts a server on network ("tcp4" or "tcp6") that answers every
// request with answer, like ip.tar.tn answers with the caller's IP.
func serveAnswer(t *testing.T, network, answer string) string {
	t.Helper()
	addr := "127.0.0.1:0"
	if network == "tcp6" {
		addr = "[::1]:0"
	}
	l, err := net.Listen(network, addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", network, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, answer)
	}))
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

// The public IP becomes the url http://<ip>:7777 and the address the control
// center shows, so only an IPv4 address is usable.
func TestGetIP(t *testing.T) {
	savedURLs := ipServiceURLs
	t.Cleanup(func() { ipServiceURLs, ipCache = savedURLs, "" })

	for _, tc := range []struct{ answer, want string }{
		{"203.0.113.7\n", "203.0.113.7"},
		{"<h1>Not Found</h1>", "Unknown"}, // an error page served with 200
		{"", "Unknown"},
		{"2001:db8::1", "Unknown"},
		{"::ffff:203.0.113.7", "Unknown"},
	} {
		ipCache = ""
		ipServiceURLs = []string{serveAnswer(t, "tcp4", tc.answer)}
		if got := getIP(); got != tc.want {
			t.Errorf("answer %q: getIP() = %q, want %q", tc.answer, got, tc.want)
		}
	}

	// When the first domain fails, the second one answers.
	ipCache = ""
	ipServiceURLs = []string{serveAnswer(t, "tcp4", "<h1>Bad Gateway</h1>"), serveAnswer(t, "tcp4", "203.0.113.9")}
	if got := getIP(); got != "203.0.113.9" {
		t.Errorf("second service not asked: %q", got)
	}

	// A dual-stack server would reach the service over IPv6 and be told its IPv6 address.
	t.Run("IPv6", func(t *testing.T) {
		ipCache = ""
		ipServiceURLs = []string{serveAnswer(t, "tcp6", "198.51.100.4")}
		if got := getIP(); got != "Unknown" {
			t.Errorf("reached the service over IPv6, got %q", got)
		}
	})
}
