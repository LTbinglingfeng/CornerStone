package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"
)

func assertDelivery(t *testing.T, err error, outcome string, retry bool) {
	t.Helper()
	var delivery *ClawBotDeliveryError
	if !errors.As(err, &delivery) || delivery.Outcome != outcome || delivery.Retryable != retry {
		t.Fatalf("unexpected delivery error: %#v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("credential leaked: %v", err)
	}
}

func TestClawBotDeliveryReceipt(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ClawBotSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Msg.ClientID != "fixed-id" {
			t.Errorf("client_id = %q", body.Msg.ClientID)
		}
		calls++
		io.WriteString(w, `{"ret":0,"message_id":18446744073709551615}`)
	}))
	defer server.Close()
	c := NewClawBotClient()
	for i := 0; i < 2; i++ {
		receipt, err := c.SendTextMessageWithID(context.Background(), server.URL, "secret", "uin", "user", "context-secret", "text", "fixed-id")
		if err != nil {
			t.Fatal(err)
		}
		if receipt.MessageID != "18446744073709551615" {
			t.Fatalf("receipt: %+v", receipt)
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

// iLink commonly acknowledges sendmessage with `{}`; like the official
// openclaw-weixin client, only a non-zero ret/errcode is a failure.
func TestClawBotDeliveryAcceptsReceiptWithoutRet(t *testing.T) {
	for _, body := range []string{`{}`, `{"ret":null}`, `{"errmsg":""}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, body)
		}))
		receipt, err := NewClawBotClient().SendTextMessageWithID(context.Background(), server.URL, "secret", "uin", "user", "context-secret", "text", "fixed")
		server.Close()
		if err != nil || receipt == nil || receipt.MessageID != "" {
			t.Fatalf("%s: receipt=%+v err=%v", body, receipt, err)
		}
	}
}

func TestClawBotDeliveryResponses(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		body, outcome string
		retry, stale  bool
	}{
		{"stale", 200, `{"ret":-14,"errmsg":"secret context-secret"}`, ClawBotDeliveryRejected, false, true},
		{"unknown", 200, `{"ret":42}`, ClawBotDeliveryRejected, false, false},
		{"errcode", 200, `{"errcode":-14}`, ClawBotDeliveryRejected, false, true},
		{"rate", 429, "secret", ClawBotDeliveryRejected, true, false},
		{"gateway", 502, "secret", ClawBotDeliveryUncertain, false, false},
		{"invalid", 200, `secret`, ClawBotDeliveryUncertain, false, false},
		{"trailing", 200, `{"ret":0}secret`, ClawBotDeliveryUncertain, false, false},
		{"unauthorized", 401, "secret", ClawBotDeliveryRejected, false, false},
		{"forbidden", 403, "secret", ClawBotDeliveryRejected, false, false},
		{"bad-request", 400, "secret", ClawBotDeliveryRejected, false, false},
		{"redirect", 307, "secret", ClawBotDeliveryRejected, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/secret")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := NewClawBotClient().SendTextMessageWithID(context.Background(), server.URL, "secret", "uin", "user", "context-secret", "text", "fixed")
			assertDelivery(t, err, tc.outcome, tc.retry)
			if IsClawBotStaleToken(err) != tc.stale {
				t.Errorf("stale classification: %v", err)
			}
			if calls != 1 {
				t.Errorf("unexpected retry/redirect: %d", calls)
			}
		})
	}
}

func TestClawBotDeliveryAfterBody(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{true: "disconnect", false: "timeout"}[disconnect], func(t *testing.T) {
			read := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				close(read)
				if disconnect {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				} else {
					<-release
				}
			}))
			defer server.Close()
			defer close(release)
			c := NewClawBotClient()
			c.httpClient.Timeout = 150 * time.Millisecond
			_, err := c.SendTextMessageWithID(context.Background(), server.URL, "secret", "uin", "user", "context-secret", "text", "fixed")
			assertDelivery(t, err, ClawBotDeliveryUncertain, false)
			select {
			case <-read:
			default:
				t.Fatal("server did not read body")
			}
		})
	}
}

type deliveryRoundTripper func(*http.Request) (*http.Response, error)

func (f deliveryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClawBotDeliveryBeforeSend(t *testing.T) {
	c := NewClawBotClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.SendTextMessageWithID(ctx, "https://example.invalid", "secret", "uin", "user", "", "text", "fixed")
	assertDelivery(t, err, ClawBotDeliveryNotSent, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation")
	}
	for _, traced := range []bool{false, true} {
		c.httpClient.Transport = deliveryRoundTripper(func(r *http.Request) (*http.Response, error) {
			if traced {
				httptrace.ContextClientTrace(r.Context()).ConnectDone("tcp", "example.invalid:443", errors.New("secret"))
			}
			return nil, errors.New("secret")
		})
		_, err = c.SendTextMessageWithID(context.Background(), "https://example.invalid", "secret", "uin", "user", "", "text", "fixed")
		outcome := ClawBotDeliveryUncertain
		if traced {
			outcome = ClawBotDeliveryNotSent
		}
		assertDelivery(t, err, outcome, traced)
	}
	_, err = c.SendTextMessageWithID(context.Background(), "https://example.invalid", "secret", "uin", "user", "", "text", " ")
	assertDelivery(t, err, ClawBotDeliveryNotSent, false)
}
