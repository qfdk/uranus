package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStaticFilesGetValidCacheHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CacheMiddleware())
	engine.GET("/public/icons/server.svg", func(c *gin.Context) {
		c.Data(http.StatusOK, "image/svg+xml", []byte("<svg/>"))
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/icons/server.svg", nil))

	// max-age used to be built with string(rune(86400)), i.e. a single unicode character.
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=86400" {
		t.Fatalf("Cache-Control = %q", got)
	}
	// The handler knows the file type; the middleware must not pre-empt it.
	if got := w.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type = %q", got)
	}
}
