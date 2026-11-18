package controllers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCertInfo(t *testing.T) {
	engine := gin.New()
	engine.GET("/ssl/info", CertInfo)

	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	w := get(engine, "/ssl/info?domain="+strings.TrimPrefix(srv.URL, "https://"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "not_after") {
		t.Fatalf("certificate of a reachable host: %d %s", w.Code, w.Body.String())
	}

	// An unreachable host used to crash the handler with a nil pointer dereference.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	ln.Close()
	if w := get(engine, "/ssl/info?domain="+closed); w.Code != http.StatusBadGateway {
		t.Fatalf("unreachable host: %d %s", w.Code, w.Body.String())
	}
}
