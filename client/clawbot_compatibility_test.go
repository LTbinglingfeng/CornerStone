package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClawBotEffectiveMessageID(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		want       ClawBotMessageID
	}{
		{"top-level wins", `{"message_id":123,"item_list":[{"msg_id":456}]}`, "123"},
		{"numeric item", `{"item_list":[{"msg_id":18446744073709551615}]}`, "18446744073709551615"},
		{"string item", `{"item_list":[{"msg_id":"18446744073709551615"}]}`, "18446744073709551615"},
		{"first usable item", `{"item_list":[{}, {"msg_id":0}, {"msg_id":123}, {"msg_id":456}]}`, "123"},
		{"zero top-level", `{"message_id":0,"item_list":[{"msg_id":123}]}`, "123"},
		{"malformed optional ID", `{"item_list":[{"msg_id":"invalid"}, {"msg_id":123}]}`, "123"},
		{"missing", `{"item_list":[{}]}`, ""},
		{"zero", `{"message_id":0,"item_list":[{"msg_id":0}]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var msg ClawBotIncomingMessage
			if err := json.Unmarshal([]byte(tc.wire), &msg); err != nil {
				t.Fatal(err)
			}
			if got := msg.EffectiveMessageID(); got != tc.want {
				t.Fatalf("ID = %q, want %q", got, tc.want)
			}
			// Durable queue serialization must preserve the same lossless ID.
			raw, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			var restored ClawBotIncomingMessage
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			if got := restored.EffectiveMessageID(); got != tc.want {
				t.Fatalf("restored ID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClawBotContextTokenSentVerbatim(t *testing.T) {
	for _, token := range []string{" \topaque-context\r\n", " \t\r\n", ""} {
		t.Run(fmt.Sprintf("token=%q", token), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					ContextToken string `json:"context_token"`
					Msg          struct {
						ContextToken string `json:"context_token"`
					} `json:"msg"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				got := body.Msg.ContextToken
				if r.URL.Path == "/ilink/bot/getconfig" {
					got = body.ContextToken
				}
				if got != token {
					t.Errorf("%s token = %q, want %q", r.URL.Path, got, token)
				}
				calls++
				fmt.Fprint(w, `{"ret":0,"message_id":"123","typing_ticket":"ticket"}`)
			}))
			defer server.Close()
			c := NewClawBotClient()
			ctx := context.Background()
			if err := c.SendTextMessage(ctx, server.URL, "bot", "uin", "user", token, "reply"); err != nil {
				t.Fatal(err)
			}
			if _, err := c.SendTextMessageWithID(ctx, server.URL, "bot", "uin", "user", token, "reply", "client-id"); err != nil {
				t.Fatal(err)
			}
			if _, err := c.GetConfig(ctx, server.URL, "bot", "uin", "user", token); err != nil {
				t.Fatal(err)
			}
			if calls != 3 {
				t.Fatalf("requests = %d, want 3", calls)
			}
		})
	}
}
