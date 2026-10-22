package client

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type clawBotProtocolTransport func(*http.Request) (*http.Response, error)

func (f clawBotProtocolTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClawBotNetworkErrorsAreClassifiedWithoutURLs(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/?qrcode=secret", nil)
	for _, tc := range []struct {
		err   error
		label string
	}{
		{&net.DNSError{Err: "lookup failed", Name: "secret"}, "dns"},
		{x509.UnknownAuthorityError{}, "tls"},
		{&net.OpError{Op: "dial", Err: errors.New("secret")}, "tcp"},
	} {
		err := clawBotSafeRequestError(req, &url.Error{Op: "Get", URL: req.URL.String(), Err: tc.err})
		if !strings.Contains(err.Error(), tc.label) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe classification: %v", err)
		}
	}
}

func TestClawBotProtocolHeadersAndBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("iLink-App-Id") != "bot" || r.Header.Get("iLink-App-ClientVersion") != "132106" {
			t.Errorf("common headers: %v", r.Header)
		}
		if r.Method == http.MethodGet {
			for _, key := range []string{"Authorization", "AuthorizationType", "X-WECHAT-UIN"} {
				if r.Header.Get(key) != "" {
					t.Errorf("GET leaked %s", key)
				}
			}
			if r.URL.Query().Get("qrcode") != "qr&secret" || r.URL.Query().Get("verify_code") != "012345" {
				t.Error("QR query mismatch")
			}
			fmt.Fprint(w, `{"status":"wait"}`)
			return
		}
		if r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("X-WECHAT-UIN") == "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing POST headers")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/ilink/bot/get_bot_qrcode" {
			if r.Header.Get("Authorization") != "" || body["base_info"] != nil {
				t.Error("QR creation should not authenticate or attach base_info")
			}
			var tokens []string
			if err := json.Unmarshal(body["local_token_list"], &tokens); err != nil || len(tokens) != 2 || tokens[0] != "local-one" {
				t.Errorf("local tokens: %s", body["local_token_list"])
			}
			fmt.Fprint(w, `{"qrcode":"qr"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer bot-secret" {
			t.Error("missing bearer")
		}
		var info ClawBotBaseInfo
		if err := json.Unmarshal(body["base_info"], &info); err != nil || info.ChannelVersion != "2.4.10" || info.BotAgent != "CornerStone/v9.1.0" {
			t.Errorf("base_info: %+v, %v", info, err)
		}
		fmt.Fprint(w, `{"ret":0,"longpolling_timeout_ms":42000}`)
	}))
	defer server.Close()
	c := NewClawBotClient("v9.1.0")
	ctx := context.Background()
	if _, err := c.GetBotQRCode(ctx, server.URL, "local-one", "local-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetQRCodeStatus(ctx, server.URL, "qr&secret", "012345"); err != nil {
		t.Fatal(err)
	}
	updates, err := c.GetUpdates(ctx, server.URL, "bot-secret", "", "uin")
	if err != nil || updates.LongPollingTimeoutMS != 42000 {
		t.Fatalf("updates=%+v err=%v", updates, err)
	}
	if err := c.SendTextMessage(ctx, server.URL, "bot-secret", "uin", "user", "context-secret", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := c.NotifyStart(ctx, server.URL, "bot-secret", "uin"); err != nil {
		t.Fatal(err)
	}
	if err := c.NotifyStop(ctx, server.URL, "bot-secret", "uin"); err != nil {
		t.Fatal(err)
	}
}

func TestClawBotAgentSanitization(t *testing.T) {
	for _, version := range []string{"", "中文", "v1\r\nInjected", strings.Repeat("a", 300), "a/b", "v1 test"} {
		agent := NewClawBotClient(version).baseInfo().BotAgent
		if agent != "CornerStone/dev" || len(agent) > 256 {
			t.Errorf("unsafe version %q => %q", version, agent)
		}
	}
	if got := NewClawBotClient().baseInfo().BotAgent; got != "CornerStone/dev" {
		t.Fatal(got)
	}
}

func TestClawBotProtocolDeadlines(t *testing.T) {
	c := NewClawBotClient()
	want := time.Duration(0)
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining > want || remaining < want-time.Second {
			t.Errorf("deadline %v want %v", remaining, want)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	ctx := context.Background()
	want = 30 * time.Second
	_, _ = c.GetBotQRCode(ctx, "https://example.test")
	want = 35 * time.Second
	_, _ = c.GetQRCodeStatus(ctx, "https://example.test", "qr")
	_, _ = c.GetUpdates(ctx, "https://example.test", "token", "", "uin")
	want = 2 * time.Second
	_, _ = c.GetUpdates(ctx, "https://example.test", "token", "", "uin", want)
	want = 15 * time.Second
	_ = c.SendTextMessage(ctx, "https://example.test", "token", "uin", "user", "context", "text")
	want = 10 * time.Second
	_ = c.NotifyStart(ctx, "https://example.test", "token", "uin")
	_ = c.NotifyStop(ctx, "https://example.test", "token", "uin")
	want = 60 * time.Second
	_, _ = c.DownloadImageItem(ctx, &ClawBotImageItem{Media: &ClawBotCDNMedia{FullURL: "https://example.test/image"}})
	want = 3 * time.Second
	parent, cancel := context.WithTimeout(ctx, want)
	defer cancel()
	_, _ = c.GetUpdates(parent, "https://example.test", "token", "", "uin")
}

func TestClawBotPollTimeoutAndCancellation(t *testing.T) {
	c := NewClawBotClient()
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	_, err := c.GetUpdates(context.Background(), "https://example.test", "token", "", "uin", time.Millisecond)
	if !errors.Is(err, ErrClawBotPollTimeout) {
		t.Fatalf("internal timeout: %v", err)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.GetUpdates(parent, "https://example.test", "token", "", "uin")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrClawBotPollTimeout) {
		t.Fatalf("cancel: %v", err)
	}
	parent, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = c.GetUpdates(parent, "https://example.test", "token", "", "uin")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrClawBotPollTimeout) {
		t.Fatalf("parent deadline: %v", err)
	}
	_, err = c.GetQRCodeStatus(parent, "https://example.test", "qr-secret", "code-secret")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("QR context: %v", err)
	}
	c.httpClient.Transport = clawBotProtocolTransport(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	_, err = c.GetUpdates(context.Background(), "https://example.test", "token", "", "uin")
	if errors.Is(err, ErrClawBotPollTimeout) {
		t.Fatal("transport timeout incorrectly classified as internal poll deadline")
	}
}

func TestClawBotProtocolErrorsAndRedaction(t *testing.T) {
	status, body := 200, `{"ret":-14,"errmsg":"bot-secret context-secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, body) }))
	defer server.Close()
	c := NewClawBotClient()
	err := c.SendTextMessage(context.Background(), server.URL, "bot-secret", "uin", "user", "context-secret", "hi")
	if !IsClawBotStaleToken(err) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("send error: %v", err)
	}
	for _, notify := range []func(context.Context, string, string, string) error{c.NotifyStart, c.NotifyStop} {
		err = notify(context.Background(), server.URL, "bot-secret", "uin")
		if !IsClawBotStaleToken(err) || strings.Contains(err.Error(), "bot-secret") {
			t.Fatalf("notify: %v", err)
		}
	}
	body = `{"errcode":-14,"errmsg":"context_token=unknown-secret bot-secret"}`
	updates, err := c.GetUpdates(context.Background(), server.URL, "bot-secret", "", "uin")
	if err != nil || !IsClawBotStaleToken(updates.APIError()) || strings.Contains(updates.APIError().Error(), "secret") {
		t.Fatalf("updates: %+v %v", updates, err)
	}
	body = `{"errcode":7,"errmsg":"qr-secret code-secret"}`
	_, err = c.GetQRCodeStatus(context.Background(), server.URL, "qr-secret", "code-secret")
	var apiErr *ClawBotAPIError
	if !errors.As(err, &apiErr) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("QR: %v", err)
	}
	for _, code := range []int{200, 400} {
		status, body = code, `invalid response qrcode=qr-secret verify_code=code-secret`
		_, err = c.GetQRCodeStatus(context.Background(), server.URL, "qr-secret", "code-secret")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid/http response: %v", err)
		}
	}
	_, err = c.GetQRCodeStatus(context.Background(), "://bad?secret", "qr-secret", "code-secret")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid URL: %v", err)
	}
}

func TestClawBotProtocolRejectsCredentialRedirects(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, `{}`) }))
	defer target.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/?qrcode=secret", code) }))
		c := NewClawBotClient()
		_, err := c.GetQRCodeStatus(context.Background(), source.URL, "qr-secret", "code-secret")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("GET redirect: %v", err)
		}
		if err := c.NotifyStart(context.Background(), source.URL, "bot-secret", "uin"); err == nil {
			t.Fatal("POST redirect accepted")
		}
		if _, err := c.GetBotQRCode(context.Background(), source.URL, "local-secret"); err == nil {
			t.Fatal("QR POST redirect accepted")
		}
		source.Close()
	}
	if hits != 0 {
		t.Fatalf("redirect target got %d requests", hits)
	}
}
