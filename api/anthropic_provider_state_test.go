package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnthropicSignedHistorySurvivesDisplayNormalization(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			var request struct {
				Messages []struct {
					Content []map[string]json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if len(request.Messages) == 0 || len(request.Messages[0].Content) != 3 {
				t.Errorf("lost signed blocks: %+v", request)
			} else {
				blocks := request.Messages[0].Content
				if string(blocks[0]["signature"]) != `"signature"` || string(blocks[1]["text"]) != `" Checking now.\n"` {
					t.Errorf("signed turn changed: %+v", blocks)
				}
			}
		}
		fmt.Fprint(w, `{"model":"claude-snapshot","content":[{"type":"thinking","thinking":"plan","signature":"signature"},{"type":"text","text":" Checking now.\n"},{"type":"tool_use","id":"call_1","name":"send_pat","input":{}}]}`)
	}))
	defer server.Close()
	c := client.NewAnthropicClient(server.URL, "test")
	resp, err := c.Chat(context.Background(), client.ChatRequest{Model: "claude-alias", Messages: []client.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	saved := buildGeneratedReplyStorageMessages(&toolLoopResult{NewMessages: []client.Message{resp.Choices[0].Message}}, "", sessionReplyPersistAll, time.Now())
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restored []storage.ChatMessage
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	messages := ensureToolResultMessagesForReplay(convertChatMessages(restored))
	if messages[0].Content != strings.TrimSpace(resp.Choices[0].Message.Content) {
		t.Fatal("fixture did not exercise normalization")
	}
	if _, err = c.Chat(context.Background(), client.ChatRequest{Model: "claude-alias", Messages: messages}); err != nil {
		t.Fatal(err)
	}
}
