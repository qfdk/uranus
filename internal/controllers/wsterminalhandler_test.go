package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uranus/internal/wsterminal"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// The web terminal is a root shell authenticated by the session cookie: a page
// from another origin, e.g. another site served from the same machine, must not
// be able to open it with the admin's cookie.
func TestTerminalWebSocketRejectsOtherOrigins(t *testing.T) {
	engine := gin.New()
	engine.GET("/ws/terminal", WebSocketTerminalHandler)
	srv := httptest.NewServer(engine)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal"

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": {"http://evil.example"}})
	if err == nil {
		conn.Close()
		t.Fatal("cross-origin WebSocket handshake accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-origin handshake, got %v", resp)
	}

	conn, _, err = websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": {srv.URL}})
	if err != nil {
		t.Fatalf("same-origin handshake rejected: %v", err)
	}
	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for len(wsterminal.GetGlobalManager().ListTerminals()) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("terminal not closed after the client left")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
