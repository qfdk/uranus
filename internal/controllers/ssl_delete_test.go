package controllers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uranus/internal/models"

	"github.com/gin-gonic/gin"
)

func postDeleteSSL(configName string) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.POST("/ssl/delete", DeleteSSL)
	req := httptest.NewRequest(http.MethodPost, "/ssl/delete", strings.NewReader(url.Values{"configName": {configName}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// issuedCert stores the record of an issued certificate and its files.
func issuedCert(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(sslDir, name)
	mustWrite(t, filepath.Join(dir, "fullchain.cer"), "cert")
	cert := models.Cert{FileName: name, Domains: name, NotAfter: time.Now().Add(60 * 24 * time.Hour)}
	if err := models.GetDbClient().Create(&cert).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cert.Remove(); os.RemoveAll(dir) })
	return dir
}

func httpsSite(t *testing.T, name, certName string) string {
	t.Helper()
	conf := filepath.Join(sitesDir, name+".conf")
	mustWrite(t, conf, "server {\n    listen 443 ssl;\n    server_name "+name+";\n"+
		"    ssl_certificate "+filepath.Join(sslDir, certName, "fullchain.cer")+";\n"+
		"    ssl_certificate_key "+filepath.Join(sslDir, certName, "private.key")+";\n}\n")
	t.Cleanup(func() { os.Remove(conf) })
	return conf
}

// Deleting a certificate that a site still uses made the next reload fail and
// took every site down with it.
func TestDeleteSSLRefusesCertificateInUse(t *testing.T) {
	dir := issuedCert(t, "blog.example.com")
	site := httpsSite(t, "www.example.com", "blog.example.com")

	w := postDeleteSSL("blog.example.com")
	location, _ := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusFound || location.Path != "/admin/ssl" ||
		!strings.Contains(location.Query().Get("error"), site) {
		t.Fatalf("delete a certificate in use: %d %q", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("certificate in use was deleted: %v", err)
	}
	if cert := models.GetCertByFilename("blog.example.com"); cert.NotAfter.IsZero() {
		t.Fatal("certificate in use was marked as deleted")
	}

	// Once no site uses it, it goes.
	os.Remove(site)
	w = postDeleteSSL("blog.example.com")
	if location, _ := url.Parse(w.Header().Get("Location")); w.Code != http.StatusFound ||
		location.Query().Get("deleted") != "blog.example.com" {
		t.Fatalf("delete an unused certificate: %d %q", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("unused certificate kept: %v", err)
	}
}

// Deleting a site removes its certificate, unless another site uses it: the
// files and the record (which is what gets renewed) stay.
func TestDeleteSiteConfKeepsCertificateUsedElsewhere(t *testing.T) {
	dir := issuedCert(t, "blog.example.com")
	site := httpsSite(t, "blog.example.com", "blog.example.com")
	other := httpsSite(t, "www.example.com", "blog.example.com")

	w := postDelete(site)
	location, _ := url.Parse(w.Header().Get("Location"))
	if w.Code != http.StatusFound || location.Query().Get("deleted") != "blog.example.com.conf" ||
		!strings.Contains(location.Query().Get("certKept"), other) {
		t.Fatalf("redirect = %d %q", w.Code, w.Header().Get("Location"))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("certificate used by %s was deleted: %v", other, err)
	}
	if models.GetCertByFilename("blog.example.com").ID == 0 {
		t.Fatal("record of a certificate in use was deleted")
	}
}
