package api

import (
	"context"
	"cornerstone/client"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestClawBotTypingLifecycleCache(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	contexts := []string{}
	statuses := make(chan int, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Context string `json:"context_token"`
			Status  int    `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path == "/ilink/bot/getconfig" {
			mu.Lock()
			gets++
			contexts = append(contexts, b.Context)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
			return
		}
		if r.URL.Path == "/ilink/bot/sendtyping" {
			statuses <- b.Status
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	wait := func(want int) {
		t.Helper()
		select {
		case got := <-statuses:
			if got != want {
				t.Fatalf("status %d want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing typing status")
		}
	}
	key := sha256.Sum256([]byte(clawBotAccount(cfg) + "\x00" + cfg.BotToken + "\x00user"))
	start := func() {
		stop := s.startClawBotTyping(context.Background(), cfg, "user")
		wait(1)
		stop()
		wait(2)
		s.stopClawBotTyping()
	}
	s.setContextToken("user", "one")
	start()
	// Allow another start without sleeping; a warm ticket must not be fetched.
	reset := func(expire bool) {
		s.typing.mu.Lock()
		e := s.typing.cache[key]
		e.sendAt = time.Time{}
		e.retryAt = time.Time{}
		if expire {
			e.expires = time.Time{}
		}
		s.typing.cache[key] = e
		s.typing.mu.Unlock()
	}
	reset(false)
	start()
	mu.Lock()
	n := gets
	mu.Unlock()
	if n != 1 {
		t.Fatalf("cache missed: %d", n)
	}
	s.setContextToken("user", "two")
	reset(false)
	start()
	reset(true)
	start()
	mu.Lock()
	defer mu.Unlock()
	if gets != 3 || contexts[1] != "two" {
		t.Fatalf("TTL/context refresh: %d %v", gets, contexts)
	}
}

func TestClawBotTypingCancellationNonBlocking(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/getconfig" {
			close(entered)
			<-release
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	defer close(release)
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := s.startClawBotTyping(ctx, cfg, "user")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("generation blocked on typing")
	}
	cancel()
	s.stopClawBotTyping()
}

func TestClawBotTypingFailureBackoffAndAccountIsolation(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ret":7}`))
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	run := func() {
		stop := s.startClawBotTyping(context.Background(), cfg, "user")
		time.Sleep(30 * time.Millisecond)
		stop()
		s.stopClawBotTyping()
	}
	run()
	run()
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("failure retry storm: %d", n)
	}
	cfg.BotToken = "other-token"
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	run()
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("account reused failed cache: %d", calls)
	}
}

func TestClawBotTypingCloseAndAccountSwitch(t *testing.T) {
	for _, action := range []string{"close", "switch", "cancel"} {
		t.Run(action, func(t *testing.T) {
			statuses := make(chan int, 8)
			auth := make(chan string, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ilink/bot/getconfig" {
					_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"old-ticket"}`))
					return
				}
				if r.URL.Path == "/ilink/bot/sendtyping" {
					var b struct {
						Status int `json:"status"`
					}
					_ = json.NewDecoder(r.Body).Decode(&b)
					auth <- r.Header.Get("Authorization")
					statuses <- b.Status
				}
				_, _ = w.Write([]byte(`{"ret":0}`))
			}))
			defer server.Close()
			s := newTestClawBotService(t, server.URL)
			cfg := s.handler.configManager.GetClawBotConfig()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stop := s.startClawBotTyping(ctx, cfg, "user")
			defer stop()
			select {
			case <-statuses:
			case <-time.After(time.Second):
				t.Fatal("no start")
			}
			<-auth
			switch action {
			case "close":
				s.Close()
			case "switch":
				cfg.Enabled = false
				cfg.BotToken = "new-token"
				if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
					t.Fatal(err)
				}
				s.ApplyCurrentConfig()
			case "cancel":
				cancel()
				s.stopClawBotTyping()
			}
			select {
			case status := <-statuses:
				if status != 2 {
					t.Fatal(status)
				}
			case <-time.After(time.Second):
				t.Fatal("no stop")
			}
			if token := <-auth; token != "Bearer bot-token" {
				t.Fatal("stop used new account credentials")
			}
		})
	}
}

func TestClawBotTypingRateLimit(t *testing.T) {
	var mu sync.Mutex
	gets, starts := 0, 0
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/getconfig" {
			mu.Lock()
			gets++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
			return
		}
		var b struct {
			Status int `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b.Status == 1 {
			mu.Lock()
			starts++
			mu.Unlock()
			select {
			case started <- struct{}{}:
			default:
			}
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	stop := s.startClawBotTyping(context.Background(), cfg, "user")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	stop()
	s.stopClawBotTyping()
	for i := 0; i < 8; i++ {
		stop = s.startClawBotTyping(context.Background(), cfg, "user")
		time.Sleep(time.Millisecond)
		stop()
		s.stopClawBotTyping()
	}
	mu.Lock()
	defer mu.Unlock()
	if gets != 1 || starts != 1 {
		t.Fatalf("unthrottled: gets=%d starts=%d", gets, starts)
	}
}

// Exercise the durable generation entry point, not just the typing helper.
func TestClawBotTypingRealGeneration(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			started := make(chan struct{}, 1)
			stopped := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ilink/bot/getconfig":
					_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
				case "/ilink/bot/sendtyping":
					var b struct {
						Status int `json:"status"`
					}
					_ = json.NewDecoder(r.Body).Decode(&b)
					if b.Status == 1 {
						select {
						case started <- struct{}{}:
						default:
						}
					} else {
						select {
						case stopped <- struct{}{}:
						default:
						}
					}
					_, _ = w.Write([]byte(`{"ret":0}`))
				case "/chat/completions":
					select {
					case <-started:
					case <-time.After(time.Second):
						t.Error("generation did not start typing")
					}
					if fail {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"message":"model failed"}}`))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"reply"},"finish_reason":"stop"}]}`))
				default:
					t.Errorf("unexpected delivery during generation: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			s, cfg, _ := deliveryTestService(t, server.URL)
			msg := newClawBotTextIncomingMessage("user", "hello")
			msg.MessageID = "901"
			if err := s.receiveDeliveryBatch(cfg, "", "cursor", []client.ClawBotIncomingMessage{msg}); err != nil {
				t.Fatal(err)
			}
			job := s.state.Delivery.Jobs[0]
			s.stepDelivery(context.Background(), cfg, job)
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("generation did not stop typing")
			}
			s.stopClawBotTyping()
			got := deliveryTestJob(t, s, job.ID)
			want := "ready"
			if fail {
				want = "uncertain"
			}
			if got.Status != want {
				t.Fatalf("delivery altered: status=%s want=%s", got.Status, want)
			}
		})
	}
}
