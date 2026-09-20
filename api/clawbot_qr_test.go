package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClawBotQRCodeVerificationAndConfirmation(t *testing.T) {
	var mu sync.Mutex
	status := "need_verifycode"
	var codes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			if r.Method != http.MethodPost {
				t.Error("QR request must use POST")
			}
			var req struct {
				Tokens []string `json:"local_token_list"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Tokens) != 1 || req.Tokens[0] != "bot-token" {
				t.Errorf("unexpected local token list")
			}
			fmt.Fprint(w, `{"qrcode":"qr-value","qrcode_img_content":"qr-url"}`)
		case "/ilink/bot/get_qrcode_status":
			mu.Lock()
			defer mu.Unlock()
			codes = append(codes, r.URL.Query().Get("verify_code"))
			if r.URL.Query().Get("qrcode") != "qr-value" {
				t.Error("missing QR value")
			}
			json.NewEncoder(w).Encode(map[string]string{"status": status, "bot_token": "new-token", "ilink_bot_id": "bot-id", "ilink_user_id": "user-id"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.Enabled = false
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	start, err := s.StartQRCode(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	poll := func(code, want string) {
		t.Helper()
		result, err := s.PollQRCode(context.Background(), start.SessionID, code)
		if err != nil || result.Status != want {
			t.Fatalf("poll = %#v, %v", result, err)
		}
	}
	poll("", "need_verifycode")
	mu.Lock()
	status = "wait"
	mu.Unlock()
	poll("123456", "wait")
	mu.Lock()
	status = "scaned"
	mu.Unlock()
	poll("", "scaned")
	mu.Lock()
	status = "confirmed"
	mu.Unlock()
	poll("", "confirmed")
	cfg = s.handler.configManager.GetClawBotConfig()
	if cfg.BotToken != "new-token" || cfg.ILinkBotID != "bot-id" || cfg.ILinkUserID != "user-id" {
		t.Fatal("credentials not saved")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(codes, ",") != ",123456,123456," {
		t.Fatalf("verification flow = %v", codes)
	}
	poll("", "expired")
}

func TestClawBotQRCodeTerminalStates(t *testing.T) {
	for _, status := range []string{"expired", "verify_code_blocked", "binded_redirect", "confirmed"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]string{"status": status})
			}))
			defer server.Close()
			s := newTestClawBotService(t, server.URL)
			s.qrSessions = map[string]clawBotQRCodeSession{"id": {BaseURL: server.URL, QRCode: "qr", CreatedAt: time.Now(), LocalBotToken: "bot-token"}}
			result, err := s.PollQRCode(context.Background(), "id")
			if status == "confirmed" {
				if err == nil {
					t.Fatal("accepted empty token")
				}
				if s.handler.configManager.GetClawBotConfig().BotToken != "bot-token" {
					t.Fatal("destroyed credential")
				}
				return
			}
			if err != nil || result.Status != status {
				t.Fatalf("poll = %#v, %v", result, err)
			}
			if status == "binded_redirect" && result.Settings == nil {
				t.Fatal("binding missing settings")
			}
			if len(s.qrSessions) != 0 {
				t.Fatal("terminal session retained")
			}
		})
	}
}

func TestClawBotQRCodeRedirectAndAttemptBounds(t *testing.T) {
	for _, raw := range []string{"https://ilinkai.weixin.qq.com", "https://region.weixin.qq.com"} {
		if !trustedClawBotRedirect(raw) {
			t.Fatalf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"https://weixin.qq.com.evil.test", "https://evil.test", "http://ilinkai.weixin.qq.com", "https://localhost", "https://127.0.0.1", "https://user@ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com:443", "https://ilinkai.weixin.qq.com?token=secret"} {
		if trustedClawBotRedirect(raw) {
			t.Fatalf("accepted %s", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"scaned_but_redirect","redirect_host":"region.weixin.qq.com"}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	s.qrSessions = map[string]clawBotQRCodeSession{"id": {BaseURL: server.URL, QRCode: "qr", CreatedAt: time.Now()}}
	result, err := s.PollQRCode(context.Background(), "id")
	if err != nil || result.Status != "scaned_but_redirect" {
		t.Fatalf("redirect = %#v, %v", result, err)
	}
	if s.qrSessions["id"].BaseURL != "https://region.weixin.qq.com" {
		t.Fatal("redirect not retained")
	}
	session := s.qrSessions["id"]
	session.VerifyAttempts = 5
	s.qrSessions["id"] = session
	result, err = s.PollQRCode(context.Background(), "id", "123456")
	if err != nil || result.Status != "verify_code_blocked" {
		t.Fatalf("attempt bound = %#v, %v", result, err)
	}
}

func TestClawBotOldQRCodeCannotReplaceSavedAccount(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"status":"confirmed","bot_token":"old-qr-token"}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	s.handler.SetClawBotService(s)
	s.qrSessions = map[string]clawBotQRCodeSession{"id": {BaseURL: server.URL, QRCode: "qr", CreatedAt: time.Now()}}
	result := make(chan *ClawBotQRCodePollResponse, 1)
	go func() {
		resp, err := s.PollQRCode(context.Background(), "id")
		if err != nil {
			t.Error(err)
		}
		result <- resp
	}()
	<-started
	body := fmt.Sprintf(`{"enabled":false,"base_url":%q,"bot_token":"replacement"}`, server.URL)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/clawbot", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	s.handler.handleClawBotSettings(recorder, req)
	close(release)
	if recorder.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", recorder.Code, recorder.Body.String())
	}
	resp := <-result
	if resp == nil || resp.Status != "expired" {
		t.Fatalf("old QR result = %#v", resp)
	}
	if s.handler.configManager.GetClawBotConfig().BotToken != "replacement" {
		t.Fatal("old QR overwrote replacement credential")
	}
}

func TestClawBotFailedVerificationIsNotReplayed(t *testing.T) {
	var codes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		codes = append(codes, r.URL.Query().Get("verify_code"))
		if len(codes) == 1 {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"status":"wait"}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	s.qrSessions = map[string]clawBotQRCodeSession{"id": {BaseURL: server.URL, QRCode: "qr", CreatedAt: time.Now()}}
	if _, err := s.PollQRCode(context.Background(), "id", "123456"); err == nil {
		t.Fatal("expected network failure")
	}
	if _, err := s.PollQRCode(context.Background(), "id"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(codes, ",") != "123456," {
		t.Fatalf("verification replayed: %v", codes)
	}
}

func TestClawBotQRCodeDoesNotShareCredentialsAcrossOrigins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tokens []string `json:"local_token_list"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tokens) != 0 {
			t.Error("credential leaked to different origin")
		}
		fmt.Fprint(w, `{"qrcode":"qr"}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, "https://ilinkai.weixin.qq.com")
	if _, err := s.StartQRCode(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
}
