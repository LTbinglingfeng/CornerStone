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

func TestResponsesReasoningHistorySurvivesDisplayNormalization(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			var request struct {
				Input []map[string]json.RawMessage `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if len(request.Input) != 4 {
				t.Errorf("lost reasoning item: %+v", request)
			} else {
				if string(request.Input[0]["encrypted_content"]) != `"encrypted"` || string(request.Input[1]["content"]) != `" Checking now.\n"` {
					t.Errorf("reasoning turn changed: %+v", request.Input)
				}
			}
		}
		fmt.Fprint(w, `{"status":"completed","model":"resolved-model","output":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"encrypted"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":" Checking now.\n"}]},{"type":"function_call","call_id":"call_1","name":"send_pat","arguments":"{}"}]}`)
	}))
	defer server.Close()
	c := client.NewResponsesClient(server.URL, "test")
	resp, err := c.Chat(context.Background(), client.ChatRequest{Model: "requested-model", Messages: []client.Message{{Role: "user", Content: "hi"}}})
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
	if _, err = c.Chat(context.Background(), client.ChatRequest{Model: "requested-model", Messages: messages}); err != nil {
		t.Fatal(err)
	}
}
