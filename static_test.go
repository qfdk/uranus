package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStaticFilesAreGzipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(staticFiles(staticFS))
	const p = "/public/vendor/ghostty-web-0.4.0/ghostty-web.js"
	want, err := staticFS.ReadFile("web" + p)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ { // the second answer comes from the cache
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Content-Type") != "application/javascript" {
			t.Fatalf("headers: %v", w.Header())
		}
		if w.Body.Len() > len(want)/2 {
			t.Fatalf("compressed %d bytes into %d", len(want), w.Body.Len())
		}
		r, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := io.ReadAll(r); !bytes.Equal(got, want) {
			t.Fatal("decompressed file differs")
		}
	}

	// Clients that don't ask for gzip get the file as is.
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
	if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatalf("plain request: encoding %q, %d bytes", w.Header().Get("Content-Encoding"), w.Body.Len())
	}
	// Images are not compressed again.
	req := httptest.NewRequest(http.MethodGet, "/public/icon/favicon.ico", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("favicon: %d %q", w.Code, w.Header().Get("Content-Encoding"))
	}
}
