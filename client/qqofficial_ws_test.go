package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// All endpoints, including token refresh, stay on the local test server.
func qqCloseTestServer(t *testing.T, code int, auths chan<- int, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	upgrader := websocket.Upgrader{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			qqTestToken(w, r, t)
		case "/gateway":
			fmt.Fprintf(w, `{"url":%q}`, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
		case "/ws":
			calls.Add(1)
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			ws.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
			var auth qqOfficialFrame
			if ws.ReadJSON(&auth) != nil {
				return
			}
			auths <- auth.Op
			// No INVALID_SESSION frame: recovery must be driven by the close alone.
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, "private-secret"), time.Now().Add(time.Second))
		}
	}))
	return srv
}

func TestQQOfficialCloseRecovery(t *testing.T) {
	codes := []int{4006, 4007, 4008, 4009, 4004, 1000}
	for code := 4900; code <= 4913; code++ {
		codes = append(codes, code)
	}
	for _, code := range codes {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls atomic.Int32
			auths := make(chan int, 2)
			srv := qqCloseTestServer(t, code, auths, &calls)
			defer srv.Close()
			c := qqTestClient(srv)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			seq := int64(42)
			session := qqOfficialSession{id: "old-session", seq: &seq}
			jobs := make(chan qqOfficialJob, 1)
			err := c.connection(ctx, &session, jobs, func() {})
			var closed *qqOfficialCloseError
			if !errors.As(err, &closed) || closed.code != code || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("close error: %v", err)
			}
			if op := <-auths; op != 6 {
				t.Fatalf("initial op = %d, want Resume", op)
			}
			want := 6
			if code == 4006 || code == 4007 || (code >= 4900 && code <= 4913) {
				want = 2
				if session.id != "" || session.seq != nil {
					t.Fatalf("stale session: %+v", session)
				}
			}
			c.connection(ctx, &session, jobs, func() {})
			if op := <-auths; op != want {
				t.Fatalf("next op = %d, want %d", op, want)
			}
		})
	}
}

func TestQQOfficialCloseStopsRun(t *testing.T) {
	for _, code := range []int{4001, 4002, 4010, 4011, 4012, 4013, 4014, 4914, 4915} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			auths := make(chan int, 8)
			srv := qqCloseTestServer(t, code, auths, &calls)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
			defer cancel()
			var statuses []QQOfficialStatus
			qqTestClient(srv).Run(ctx, nil, func(s QQOfficialStatus) { statuses = append(statuses, s) })
			if ctx.Err() != nil {
				t.Fatal("Run waited for cancellation instead of stopping on terminal close")
			}
			if calls.Load() != 1 {
				t.Fatalf("connections = %d", calls.Load())
			}
			if len(statuses) != 3 || statuses[0].Status != "connecting" || statuses[1].Status != "error" || statuses[2].Status != "disconnected" {
				t.Fatalf("statuses: %+v", statuses)
			}
			if strings.Contains(statuses[1].LastError, "private-secret") {
				t.Fatal("close reason leaked")
			}
		})
	}
}
