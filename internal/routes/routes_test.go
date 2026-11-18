package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

const (
	testUUID     = "test-uuid"
	testToken    = "test-token-8d1f2e3c4b5a"
	testUser     = "admin"
	testPassword = "correct horse battery"
)

var testConfigFile string

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "uranus-routes-test")
	if err != nil {
		panic(err)
	}
	cfg := "uuid = \"" + testUUID + "\"\nip = \"127.0.0.1\"\ntoken = \"" + testToken + "\"\n" +
		"username = \"" + testUser + "\"\npassword = \"" + testPassword + "\"\n" +
		"email = \"admin@example.com\"\nsessionsecret = \"" + strings.Repeat("ab", 32) + "\"\n"
	testConfigFile = filepath.Join(dir, "config.toml")
	if err := os.WriteFile(testConfigFile, []byte(cfg), 0o600); err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func newEngine() *gin.Engine {
	engine := gin.New()
	RegisterRoutes(engine)
	return engine
}

func serve(engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == "uranus" {
			return c
		}
	}
	t.Fatalf("no session cookie in response, headers: %v", w.Header())
	return nil
}

// forgeSessionCookie signs a logged-in session with key, as anyone knowing the key can.
func forgeSessionCookie(t *testing.T, key string) *http.Cookie {
	t.Helper()
	forger := gin.New()
	forger.Use(sessions.Sessions("uranus", cookie.NewStore([]byte(key))))
	forger.GET("/", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("login", true)
		_ = s.Save()
	})
	return sessionCookie(t, serve(forger, httptest.NewRequest(http.MethodGet, "/", nil)))
}

func login(t *testing.T, engine *gin.Engine) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {testUser}, "password": {testPassword}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := serve(engine, req)
	if loc := w.Header().Get("Location"); w.Code != http.StatusFound || loc != "/admin/dashboard" {
		t.Fatalf("login failed: %d %q", w.Code, loc)
	}
	return sessionCookie(t, w)
}

// The session cookie used to be signed with the public key "secret", so anyone
// could mint an admin session and open the root web terminal.
func TestForgedSessionCookieIsRejected(t *testing.T) {
	engine := newEngine()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/terminal/info", nil)
	req.AddCookie(forgeSessionCookie(t, "secret"))
	w := serve(engine, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("forged session reached the admin area: %d %s", w.Code, w.Body.String())
	}
}

func TestLoginSessionCookie(t *testing.T) {
	engine := newEngine()
	c := login(t, engine)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie should be HttpOnly and SameSite=Lax: %+v", c)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/terminal/info", nil)
	req.AddCookie(c)
	if w := serve(engine, req); w.Code != http.StatusOK {
		t.Fatalf("logged-in request rejected: %d", w.Code)
	}

	form := url.Values{"username": {testUser}, "password": {"wrong"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if w := serve(engine, req); w.Header().Get("Location") != "/?error=1" {
		t.Fatalf("wrong password accepted: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func postJSON(engine *gin.Engine, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return serve(engine, req)
}

func configFileContains(t *testing.T, s string) bool {
	t.Helper()
	content, err := os.ReadFile(testConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(content), s)
}

// The UUID is public (GET /info), so it must not be enough to rewrite the
// configuration, which holds the admin credentials.
func TestUpdateConfigRequiresToken(t *testing.T) {
	engine := newEngine()

	w := postJSON(engine, "/update-config", `{"uuid":"`+testUUID+`","email":"attacker@example.com"}`)
	if w.Code != http.StatusUnauthorized || configFileContains(t, "attacker@example.com") {
		t.Fatalf("config updated with the UUID alone: %d %s", w.Code, w.Body.String())
	}

	w = postJSON(engine, "/update-config", `{"uuid":"`+testUUID+`","token":"guess","email":"attacker@example.com"}`)
	if w.Code != http.StatusUnauthorized || configFileContains(t, "attacker@example.com") {
		t.Fatalf("config updated with a wrong token: %d %s", w.Code, w.Body.String())
	}

	w = postJSON(engine, "/update-config", `{"uuid":"`+testUUID+`","token":"`+testToken+`","email":"ops@example.com"}`)
	if w.Code != http.StatusOK || !configFileContains(t, "ops@example.com") {
		t.Fatalf("authorized update failed: %d %s", w.Code, w.Body.String())
	}
	if !configFileContains(t, testToken) {
		t.Fatal("the token used for authentication must not be rewritten")
	}
}

// stubUpgrade replaces the real upgrade and reports each call on the returned channel.
func stubUpgrade(t *testing.T, checkErr error) <-chan struct{} {
	t.Helper()
	called := make(chan struct{}, 10)
	oldCheck, oldRun := checkSelfUpdate, runUpgrade
	checkSelfUpdate = func() error { return checkErr }
	runUpgrade = func() error { called <- struct{}{}; return nil }
	t.Cleanup(func() { checkSelfUpdate, runUpgrade = oldCheck, oldRun })
	return called
}

func expectUpgrade(t *testing.T, called <-chan struct{}, want bool) {
	t.Helper()
	select {
	case <-called:
		if !want {
			t.Fatal("upgrade started")
		}
	case <-time.After(200 * time.Millisecond):
		if want {
			t.Fatal("upgrade not started")
		}
	}
}

func TestUpgradeRequiresLoginOrToken(t *testing.T) {
	engine := newEngine()
	called := stubUpgrade(t, nil)

	// An empty body used to be treated as a trusted local call.
	if w := postJSON(engine, "/upgrade", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous upgrade: got %d", w.Code)
	}
	expectUpgrade(t, called, false)

	if w := postJSON(engine, "/upgrade", `{"token":"guess"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("upgrade with wrong token: got %d", w.Code)
	}
	expectUpgrade(t, called, false)

	if w := postJSON(engine, "/upgrade", `{"token":"`+testToken+`"}`); w.Code != http.StatusOK {
		t.Fatalf("upgrade with token: got %d %s", w.Code, w.Body.String())
	}
	expectUpgrade(t, called, true)

	// The dashboard button posts an empty body with the admin session.
	if w := postJSON(engine, "/upgrade", "", login(t, engine)); w.Code != http.StatusOK {
		t.Fatalf("upgrade from dashboard: got %d %s", w.Code, w.Body.String())
	}
	expectUpgrade(t, called, true)
}

func TestUpgradeRefusedWhereUnsupported(t *testing.T) {
	engine := newEngine()
	called := stubUpgrade(t, errors.New("在线升级仅支持 Linux"))

	w := postJSON(engine, "/upgrade", "", login(t, engine))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "仅支持 Linux") {
		t.Fatalf("expected the platform error, got %d %s", w.Code, w.Body.String())
	}
	expectUpgrade(t, called, false)
}
