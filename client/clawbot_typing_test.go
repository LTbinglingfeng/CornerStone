package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClawBotTypingProtocol(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("bad request headers")
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["ilink_user_id"] != "user" || body["base_info"] == nil {
			t.Error("missing identity/base_info")
		}
		switch r.URL.Path {
		case "/ilink/bot/getconfig":
			if body["context_token"] != "context" {
				t.Error("missing context")
			}
			_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
		case "/ilink/bot/sendtyping":
			if body["typing_ticket"] != "ticket" || (body["status"] != float64(1) && body["status"] != float64(2)) {
				t.Error("bad typing payload")
			}
			_, _ = w.Write([]byte(`{"ret":0}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClawBotClient()
	ticket, err := c.GetConfig(context.Background(), server.URL, "token", "uin", "user", "context")
	if err != nil || ticket != "ticket" {
		t.Fatalf("ticket %q err %v", ticket, err)
	}
	for _, status := range []int{1, 2} {
		if err := c.SendTyping(context.Background(), server.URL, "token", "uin", "user", ticket, status); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}

func TestClawBotTypingErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14,"errmsg":"secret-ticket secret-context"}`))
	}))
	defer server.Close()
	c := NewClawBotClient()
	_, err := c.GetConfig(context.Background(), server.URL, "token", "uin", "user", "")
	if !IsClawBotStaleToken(err) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe/missing business error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SendTyping(ctx, server.URL, "token", "uin", "user", "ticket", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := c.SendTyping(context.Background(), server.URL, "token", "uin", "user", "", 1); err == nil {
		t.Fatal("empty ticket accepted")
	}
}
