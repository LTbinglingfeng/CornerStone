package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClawBotStaleTokenStopsPolling(t *testing.T) {
	for _, field := range []string{"ret", "errcode"} {
		t.Run(field, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				fmt.Fprintf(w, `{"%s":-14,"errmsg":"expired"}`, field)
			}))
			defer server.Close()
			s := newTestClawBotService(t, server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			s.pollLoop(ctx, s.handler.configManager.GetClawBotConfig())
			if requests.Load() != 1 {
				t.Fatalf("requests = %d", requests.Load())
			}
			settings, _ := s.GetSettings()
			if settings.Status != "token_expired" {
				t.Fatalf("status = %s", settings.Status)
			}
			s.ApplyCurrentConfig()
			if s.workerRunning {
				t.Fatal("saving same credential restarted polling")
			}
			if err := s.sendTextReply(ctx, s.handler.configManager.GetClawBotConfig(), "user", "hello"); err == nil {
				t.Fatal("stale credential allowed send")
			}
			if requests.Load() != 1 {
				t.Fatal("stale credential made another request")
			}
		})
	}
}

func TestClawBotPollBusinessErrorAndCancellation(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":7,"errmsg":"failure"}`)
		called <- struct{}{}
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.pollLoop(ctx, s.handler.configManager.GetClawBotConfig()) }()
	<-called
	// Wait for the observable error, not an arbitrary network timing assumption.
	deadline := time.Now().Add(time.Second)
	for {
		settings, _ := s.GetSettings()
		if settings.Status == "error" {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("business error not exposed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry sleep ignored cancellation")
	}
}

func TestClawBotConnectionStatusAndRetryDelay(t *testing.T) {
	s := newTestClawBotService(t, "http://localhost")
	s.workerRunning = true
	settings, _ := s.GetSettings()
	if settings.Status != "connecting" {
		t.Fatalf("status = %s", settings.Status)
	}
	s.connected = true
	settings, _ = s.GetSettings()
	if settings.Status != "running" {
		t.Fatalf("status = %s", settings.Status)
	}
	s.setLastError(fmt.Errorf("network unavailable"))
	settings, _ = s.GetSettings()
	if settings.Status != "error" {
		t.Fatalf("status = %s", settings.Status)
	}
	for i, want := range []time.Duration{2, 4, 8, 16, 30, 30} {
		if got := clawBotRetryDelay(i + 1); got != want*time.Second {
			t.Fatalf("delay = %s", got)
		}
	}
	s.staleToken = "old-token"
	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.Enabled = false
	s.applyConfig(cfg)
	if s.staleToken != "" {
		t.Fatal("new credential retained stale state")
	}
}

func TestClawBotSendStaleTokenStopsChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":-14,"errmsg":"expired"}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	if err := s.sendTextReply(context.Background(), cfg, "user", "hello"); err == nil {
		t.Fatal("expected send error")
	}
	settings, _ := s.GetSettings()
	if settings.Status != "token_expired" {
		t.Fatalf("status = %s", settings.Status)
	}
	// An old worker cannot invalidate a replacement credential.
	cfg.BotToken = "replacement"
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s.staleToken = ""
	s.markTokenStale("bot-token")
	if s.staleToken != "" {
		t.Fatal("old account invalidated current account")
	}
}
