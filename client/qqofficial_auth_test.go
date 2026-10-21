package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestQQOfficialUnauthorizedRefreshesNextOperationWithoutRetry(t *testing.T) {
	var tokens, sends atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokens.Add(1)
			qqTestToken(w, r, t)
			return
		}
		if sends.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"id":"sent"}`)
	}))
	defer srv.Close()
	c := qqTestClient(srv)
	if _, err := c.SendText(context.Background(), "peer", "incoming", 1, "hello"); err == nil {
		t.Fatal("expected authentication error")
	}
	if sends.Load() != 1 || tokens.Load() != 1 {
		t.Fatal("first operation retried")
	}
	if _, err := c.SendText(context.Background(), "peer", "another-event", 1, "new reply"); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 2 || tokens.Load() != 2 {
		t.Fatal("subsequent operation did not refresh its token")
	}
}

func TestQQOfficialWebSocketAuthCloseInvalidatesToken(t *testing.T) {
	var srv *httptest.Server
	var tokens atomic.Int32
	upgrader := websocket.Upgrader{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokens.Add(1)
			qqTestToken(w, r, t)
		case "/gateway":
			fmt.Fprintf(w, `{"url":%q}`, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
		case "/ws":
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			ws.SetReadDeadline(time.Now().Add(time.Second))
			ws.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
			var auth any
			if ws.ReadJSON(&auth) != nil {
				return
			}
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "private-secret"), time.Now().Add(time.Second))
		}
	}))
	defer srv.Close()
	c := qqTestClient(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.connection(ctx, &qqOfficialSession{}, make(chan qqOfficialJob, 1), func() {})
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unsafe close error: %v", err)
	}
	if _, err := c.accessToken(ctx); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 {
		t.Fatal("authentication close kept cached token")
	}
}
