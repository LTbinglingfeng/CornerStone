package api

import (
	"context"
	"cornerstone/storage"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestClawBotLifecycleNotificationsAndShutdown(t *testing.T) {
	for _, startFails := range []bool{false, true} {
		t.Run(fmt.Sprint(startFails), func(t *testing.T) {
			var mu sync.Mutex
			var events []string
			polling := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ilink/bot/msg/notifystart":
					mu.Lock()
					events = append(events, "start")
					mu.Unlock()
					if startFails {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					fmt.Fprint(w, `{"ret":0}`)
				case "/ilink/bot/getupdates":
					_, _ = io.Copy(io.Discard, r.Body)
					polling <- struct{}{}
					<-r.Context().Done()
				case "/ilink/bot/msg/notifystop":
					if r.Context().Err() != nil {
						t.Error("stop inherited cancelled worker context")
					}
					mu.Lock()
					events = append(events, "stop")
					mu.Unlock()
					fmt.Fprint(w, `{"ret":0}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			base := newTestClawBotService(t, server.URL)
			s := NewClawBotService(base.handler, storage.NewClawBotStateManager(t.TempDir()), "v-test")
			t.Cleanup(s.Close)
			select {
			case <-polling:
			case <-time.After(2 * time.Second):
				t.Fatal("poll never started")
			}
			// Updating persona/options on the same account must not discard pending replies.
			s.ApplyCurrentConfig()
			closed := make(chan struct{})
			go func() { s.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("close did not interrupt poll")
			}
			select {
			case <-s.cleanupDone:
			default:
				t.Fatal("cleanup goroutine still running")
			}
			s.ApplyCurrentConfig()
			s.Close()
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(events, []string{"start", "stop"}) {
				t.Fatalf("lifecycle events = %v", events)
			}
		})
	}
}

func TestClawBotStaleWorkerSkipsOfflineRequest(t *testing.T) {
	var mu sync.Mutex
	var events []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		events = append(events, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/ilink/bot/getupdates" {
			fmt.Fprint(w, `{"ret":-14}`)
			return
		}
		fmt.Fprint(w, `{"ret":0}`)
	}))
	defer server.Close()
	base := newTestClawBotService(t, server.URL)
	s := NewClawBotService(base.handler, storage.NewClawBotStateManager(t.TempDir()))
	defer s.Close()
	s.mu.RLock()
	done := s.workerDone
	s.mu.RUnlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stale worker did not stop")
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(events, []string{"/ilink/bot/msg/notifystart", "/ilink/bot/getupdates"}) {
		t.Fatalf("events=%v", events)
	}
}

func TestClawBotPollTimeoutBounds(t *testing.T) {
	for _, tc := range []struct {
		ms   int
		want time.Duration
	}{{0, 35 * time.Second}, {-1, 35 * time.Second}, {1, time.Second}, {35000, 35 * time.Second}, {120001, 2 * time.Minute}, {int(^uint(0) >> 1), 2 * time.Minute}} {
		if got := clawBotPollTimeout(tc.ms, 35*time.Second); got != tc.want {
			t.Fatalf("%d => %s", tc.ms, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newTestClawBotService(t, "http://localhost")
	done := make(chan struct{})
	go func() { s.cleanupLoop(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup ignored cancellation")
	}
}
