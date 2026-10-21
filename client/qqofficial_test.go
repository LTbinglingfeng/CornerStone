package client

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func qqTestClient(s *httptest.Server) *QQOfficialClient {
	c := NewQQOfficialClient("123", "private-secret")
	c.httpClient = s.Client()
	c.tokenURL = s.URL + "/token"
	c.apiURL = s.URL
	c.gatewayAllowed = func(raw string) bool { return raw == "ws"+strings.TrimPrefix(s.URL, "http")+"/ws" }
	return c
}
func qqTestToken(w http.ResponseWriter, r *http.Request, t *testing.T) {
	var p map[string]string
	_ = json.NewDecoder(r.Body).Decode(&p)
	if p["appId"] != "123" || p["clientSecret"] != "private-secret" || r.Header.Get("Authorization") != "" {
		t.Error("bad token request")
	}
	fmt.Fprint(w, `{"access_token":"test-token","expires_in":"7200"}`)
}
func TestQQOfficialREST(t *testing.T) {
	var tokens, posts, files atomic.Int32
	var reject atomic.Bool
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokens.Add(1)
			qqTestToken(w, r, t)
			return
		}
		if r.Header.Get("Authorization") != "QQBot test-token" {
			t.Error("missing authorization")
		}
		var p map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&p)
		switch r.URL.Path {
		case "/v2/users/openid/files":
			files.Add(1)
			var encoded string
			_ = json.Unmarshal(p["file_data"], &encoded)
			data, _ := base64.StdEncoding.DecodeString(encoded)
			if !bytes.Equal(data, png) || string(p["srv_send_msg"]) != "false" || string(p["file_type"]) != "1" || string(p["file_name"]) != `"a.png"` {
				t.Error("invalid upload")
			}
			fmt.Fprint(w, `{"file_info":"media-info"}`)
		case "/v2/users/openid/messages":
			posts.Add(1)
			if reject.Load() {
				w.WriteHeader(502)
				fmt.Fprint(w, "test-token private-secret")
				return
			}
			if string(p["msg_seq"]) != "2" || string(p["msg_id"]) != `"reply"` {
				t.Error("missing reply correlation")
			}
			if string(p["msg_type"]) == "7" && string(p["media"]) != `{"file_info":"media-info"}` {
				t.Error("missing file info")
			}
			fmt.Fprint(w, `{"id":90071992547409931234}`)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	c := qqTestClient(s)
	id, err := c.SendText(context.Background(), "openid", "reply", 2, "hello")
	if err != nil || id != "90071992547409931234" {
		t.Fatalf("text: %q %v", id, err)
	}
	if _, err = c.SendImage(context.Background(), "openid", "reply", 2, png, "a.png"); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 1 || files.Load() != 1 || posts.Load() != 2 {
		t.Fatal("cache or request counts")
	}
	reject.Store(true)
	_, err = c.SendText(context.Background(), "openid", "reply", 2, "hello")
	if err == nil || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "private-secret") || posts.Load() != 3 {
		t.Fatal("uncertain POST retried or leaked")
	}
	c.tokenMu.Lock()
	c.tokenUntil = time.Now().Add(-time.Second)
	c.tokenMu.Unlock()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = c.accessToken(context.Background()) })
	}
	wg.Wait()
	if tokens.Load() != 2 {
		t.Fatal("refresh not coalesced")
	}
	if _, err = c.SendImage(context.Background(), "openid", "reply", 1, make([]byte, qqOfficialImageLimit+1), "x"); err == nil {
		t.Fatal("oversized image accepted")
	}
}

func TestQQOfficialHTTPValidation(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"id":""}`, `{"id":"ok","code":1}`, `{"id":"ok"} trailing`, `[]`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					qqTestToken(w, r, t)
					return
				}
				fmt.Fprint(w, body)
			}))
			defer s.Close()
			if _, err := qqTestClient(s).SendText(context.Background(), "u", "r", 1, "x"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	var out any
	if err := qqOfficialJSON(context.Background(), source.Client(), "POST", source.URL, "secret", map[string]int{"x": 1}, &out); err == nil || followed.Load() {
		t.Fatal("redirect followed")
	}
	for _, u := range []string{"http://example.com/i.png", "https://127.0.0.1/i", "https://[::1]/i", "https://localhost/i", "https://user:pass@example.com/i"} {
		if _, err := DownloadQQOfficialImage(context.Background(), u); err == nil {
			t.Fatal("unsafe download accepted")
		}
	}
	for _, u := range []string{"ws://api.sgroup.qq.com/x", "wss://evil.example/x", "wss://api.sgroup.qq.com.evil/x", "wss://api.sgroup.qq.com:444/x", "wss://a@api.sgroup.qq.com/x"} {
		if qqOfficialGatewayAllowed(u) {
			t.Fatal("unsafe gateway accepted")
		}
	}
	for _, gateway := range []string{"wss://api.sgroup.qq.com/websocket/", "wss://api.bot.qq.com/websocket/"} {
		if !qqOfficialGatewayAllowed(gateway) {
			t.Fatal("official gateway rejected")
		}
	}
	defaults := NewQQOfficialClient("123", "secret")
	if defaults.apiURL != "https://api.bot.qq.com" || defaults.tokenURL != "https://api.bot.qq.com/app/getAppAccessToken" {
		t.Fatal("not using current unified official API")
	}
}

func TestQQOfficialBinding(t *testing.T) {
	var key []byte
	var status = 2
	tamper := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("binding used authorization")
		}
		var p map[string]string
		_ = json.NewDecoder(r.Body).Decode(&p)
		if r.URL.Path == "/lite/create_bind_task" {
			key, _ = base64.StdEncoding.DecodeString(p["key"])
			fmt.Fprint(w, `{"retcode":0,"data":{"task_id":"a&b","expires_in":120}}`)
			return
		}
		if p["task_id"] != "a&b" {
			t.Error("wrong task id")
		}
		block, _ := aes.NewCipher(key)
		gcm, _ := cipher.NewGCM(block)
		nonce := make([]byte, 12)
		raw := gcm.Seal(nonce, nonce, []byte("app-secret"), nil)
		if tamper {
			raw[len(raw)-1] ^= 1
		}
		fmt.Fprintf(w, `{"retcode":0,"data":{"status":%d,"bot_appid":90071992547409931234,"bot_encrypt_secret":%q}}`, status, base64.StdEncoding.EncodeToString(raw))
	}))
	defer s.Close()
	b, err := qqOfficialStartBinding(context.Background(), s.Client(), s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Key) != 32 || !strings.Contains(b.URL, "task_id=a%26b&_wv=2") {
		t.Fatal("bad binding")
	}
	encoded, _ := json.Marshal(b)
	if bytes.Contains(encoded, []byte(base64.StdEncoding.EncodeToString(key))) || bytes.Contains(encoded, []byte(`"Key"`)) {
		t.Fatal("key exposed")
	}
	r, err := qqOfficialPollBinding(context.Background(), s.Client(), s.URL, b)
	if err != nil || r.Status != "created" || r.AppID != "90071992547409931234" || r.AppSecret != "app-secret" {
		t.Fatalf("result: %v", err)
	}
	encoded, _ = json.Marshal(r)
	if bytes.Contains(encoded, []byte("app-secret")) {
		t.Fatal("secret exposed")
	}
	tamper = true
	if _, err = qqOfficialPollBinding(context.Background(), s.Client(), s.URL, b); err == nil {
		t.Fatal("tampering accepted")
	}
	tamper = false
	for _, st := range []int{0, 1, 3} {
		status = st
		r, err = qqOfficialPollBinding(context.Background(), s.Client(), s.URL, b)
		if err != nil {
			t.Fatal(err)
		}
		want := "pending"
		if st == 3 {
			want = "expired"
		}
		if r.Status != want {
			t.Fatal("wrong status")
		}
	}
	b.ExpiresAt = time.Now().Add(-time.Second)
	s.Close()
	r, err = qqOfficialPollBinding(context.Background(), s.Client(), s.URL, b)
	if err != nil || r.Status != "expired" {
		t.Fatal("local expiry not applied")
	}
	for _, raw := range []string{"!", "", base64.StdEncoding.EncodeToString(make([]byte, 28))} {
		if _, err := qqOfficialDecryptSecret(raw, key); err == nil {
			t.Fatal("invalid ciphertext accepted")
		}
	}
}

func TestQQOfficialWebSocketResumeAndFiltering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var connections, messages atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var server *httptest.Server
	upgrader := websocket.Upgrader{}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			qqTestToken(w, r, t)
		case "/gateway":
			if r.Header.Get("Authorization") != "QQBot test-token" {
				t.Error("gateway auth")
			}
			fmt.Fprintf(w, `{"url":%q}`, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws")
		case "/ws":
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			_ = ws.SetReadDeadline(time.Now().Add(8 * time.Second))
			n := connections.Add(1)
			_ = ws.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 30}})
			var auth struct {
				Op int `json:"op"`
				D  struct {
					Token   string `json:"token"`
					Intents int    `json:"intents"`
					Seq     int64  `json:"seq"`
					Session string `json:"session_id"`
				} `json:"d"`
			}
			if ws.ReadJSON(&auth) != nil {
				return
			}
			if auth.D.Token != "QQBot test-token" {
				t.Error("WS token")
			}
			if n == 2 {
				if auth.Op != 6 || auth.D.Seq != 3 || auth.D.Session != "session" {
					t.Errorf("bad resume %+v", auth)
				}
				// The official gateway replays missed events before RESUMED.
				_ = ws.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 4, "d": map[string]any{"id": "replayed", "author": map[string]string{"user_openid": "u"}, "content": "hello", "timestamp": "2026-01-01T00:00:00Z"}})
				_ = ws.WriteJSON(map[string]any{"op": 0, "t": "RESUMED", "s": 5, "d": map[string]any{}})
				for {
					var hb struct {
						Op int   `json:"op"`
						D  int64 `json:"d"`
					}
					if ws.ReadJSON(&hb) != nil {
						return
					}
					_ = ws.WriteJSON(map[string]any{"op": 11})
					if hb.Op == 1 && hb.D == 5 {
						break
					}
				}
				_ = ws.WriteJSON(map[string]any{"op": 9, "d": false})
				return
			}
			if auth.Op != 2 || auth.D.Intents != 1<<25 {
				t.Error("bad identify")
			}
			_ = ws.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]any{"session_id": "session", "user": map[string]string{"id": "bot", "username": "name"}}})
			if n >= 3 {
				<-ctx.Done()
				return
			}
			_ = ws.WriteJSON(map[string]any{"op": 0, "t": "GROUP_AT_MESSAGE_CREATE", "s": 2, "d": map[string]string{"content": "ignore"}})
			_ = ws.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 3, "d": map[string]any{"id": "m", "author": map[string]string{"user_openid": "u"}, "content": "hello", "timestamp": "2026-01-01T00:00:00Z"}})
			sawBlocked := false
			for {
				var heartbeat struct {
					Op int    `json:"op"`
					D  *int64 `json:"d"`
				}
				if ws.ReadJSON(&heartbeat) != nil {
					return
				}
				if heartbeat.Op != 1 {
					t.Error("expected heartbeat")
					return
				}
				_ = ws.WriteJSON(map[string]any{"op": 11})
				if heartbeat.D != nil && *heartbeat.D < 3 {
					sawBlocked = true
					releaseOnce.Do(func() { close(release) })
				}
				if heartbeat.D != nil && *heartbeat.D == 3 {
					if !sawBlocked {
						t.Error("advanced before acceptance")
					}
					_ = ws.WriteJSON(map[string]any{"op": 7})
					return
				}
			}
		}
	}))
	defer server.Close()
	c := qqTestClient(server)
	c.Run(ctx, func(ctx context.Context, m QQOfficialMessage) error {
		messages.Add(1)
		if (m.ID != "m" && m.ID != "replayed") || m.UserOpenID != "u" || m.Content != "hello" || m.Timestamp.IsZero() {
			t.Error("bad message")
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, func(s QQOfficialStatus) {
		if s.Status == "connected" && connections.Load() >= 3 {
			cancel()
		}
	})
	if connections.Load() != 3 || messages.Load() != 2 {
		t.Fatalf("connections=%d messages=%d", connections.Load(), messages.Load())
	}
}

func TestQQOfficialWebSocketCancellationAndACKTimeout(t *testing.T) {
	for _, blockHandler := range []bool{false, true} {
		t.Run(fmt.Sprint(blockHandler), func(t *testing.T) {
			var server *httptest.Server
			upgrader := websocket.Upgrader{}
			started := make(chan struct{})
			var once sync.Once
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					qqTestToken(w, r, t)
				case "/gateway":
					fmt.Fprintf(w, `{"url":%q}`, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws")
				case "/ws":
					ws, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer ws.Close()
					_ = ws.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 20}})
					var auth any
					if ws.ReadJSON(&auth) != nil {
						return
					}
					_ = ws.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]any{"session_id": "s"}})
					if blockHandler {
						_ = ws.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 2, "d": map[string]any{"id": "m", "author": map[string]string{"user_openid": "u"}}})
					}
					for {
						var f any
						if ws.ReadJSON(&f) != nil {
							return
						}
					} // deliberately no heartbeat ACK
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := qqTestClient(server)
			done := make(chan struct{})
			go func() {
				defer close(done)
				c.Run(ctx, func(ctx context.Context, m QQOfficialMessage) error {
					once.Do(func() { close(started) })
					<-ctx.Done()
					return ctx.Err()
				}, func(s QQOfficialStatus) {
					if s.Status == "error" && strings.Contains(s.LastError, "acknowledgement") {
						once.Do(func() { close(started) })
						cancel()
					}
				})
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("callback or heartbeat timeout missing")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Run did not cancel")
			}
		})
	}
}

func TestQQOfficialRefreshWaitCancellation(t *testing.T) {
	c := NewQQOfficialClient("123", "secret")
	c.tokenGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.accessToken(ctx); err == nil {
			t.Error("cancelled refresh succeeded")
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh wait blocked cancellation")
	}
	<-c.tokenGate
	c.runGate <- struct{}{}
	c.Run(ctx, nil, nil)
	<-c.runGate
}

func TestQQOfficialUncertainTransportNotRetried(t *testing.T) {
	var posts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			qqTestToken(w, r, t)
			return
		}
		posts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer s.Close()
	if _, err := qqTestClient(s).SendText(context.Background(), "user", "reply", 1, "hello"); err == nil || posts.Load() != 1 {
		t.Fatal("uncertain delivery was retried")
	}
}

func TestQQOfficialConnectionBounds(t *testing.T) {
	for _, mode := range []string{"queue", "frame"} {
		t.Run(mode, func(t *testing.T) {
			var s *httptest.Server
			upgrader := websocket.Upgrader{}
			s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					qqTestToken(w, r, t)
				case "/gateway":
					fmt.Fprintf(w, `{"url":%q}`, "ws"+strings.TrimPrefix(s.URL, "http")+"/ws")
				case "/ws":
					ws, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer ws.Close()
					_ = ws.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
					var auth any
					if ws.ReadJSON(&auth) != nil {
						return
					}
					_ = ws.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "s"}})
					if mode == "frame" {
						_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"op":0,"d":"`+strings.Repeat("x", (1<<20)+1)+`"}`))
					} else {
						_ = ws.WriteJSON(map[string]any{"op": 0, "t": "GROUP_AT_MESSAGE_CREATE", "s": 2, "d": nil})
					}
					var end any
					_ = ws.ReadJSON(&end)
				}
			}))
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			jobs := make(chan qqOfficialJob, 1)
			jobs <- qqOfficialJob{}
			err := qqTestClient(s).connection(ctx, &qqOfficialSession{}, jobs, func() {})
			if err == nil || ctx.Err() != nil {
				t.Fatalf("bound did not fail promptly: %v", err)
			}
			if mode == "queue" && !strings.Contains(err.Error(), "queue full") {
				t.Fatal(err)
			}
		})
	}
}
