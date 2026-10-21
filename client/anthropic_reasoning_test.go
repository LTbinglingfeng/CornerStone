package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const anthropicSignedBlocks = `[{"type":"thinking","thinking":"private reasoning","signature":"signed-value","future":{"opaque":123}},{"type":"redacted_thinking","data":"encrypted-value","future_flag":true},{"type":"text","text":"answer","citations":[]},{"type":"tool_use","id":"tool_1","name":"lookup","input":{"n":1},"extension":"keep"}]`

func TestAnthropicReasoningNonstreamReplay(t *testing.T) {
	for _, responseModel := range []string{"claude-test", "claude-test-20251001", ""} {
		t.Run("model="+responseModel, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"id":"msg_1","role":"assistant","model":%q,"content":%s}`, responseModel, anthropicSignedBlocks)
			}))
			defer server.Close()
			c := NewAnthropicClient(server.URL, "test")
			resp, err := c.Chat(context.Background(), ChatRequest{Model: "claude-test"})
			if err != nil {
				t.Fatal(err)
			}
			msg := resp.Choices[0].Message
			if msg.ReasoningContent != "private reasoning" || msg.ProviderState == nil || msg.ProviderState.Model != "claude-test" {
				t.Fatalf("missing state: %+v", msg)
			}
			assertAnthropicReplay(t, msg, anthropicSignedBlocks)
			// JSON persistence must retain the opaque blocks too.
			saved, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			var restored Message
			if err := json.Unmarshal(saved, &restored); err != nil {
				t.Fatal(err)
			}
			assertAnthropicReplay(t, restored, anthropicSignedBlocks)
		})
	}
}

func assertAnthropicReplay(t *testing.T, msg Message, want string) {
	t.Helper()
	req, err := buildAnthropicRequest(ChatRequest{Model: "claude-test", Messages: []Message{msg}, PromptCaching: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req.Messages[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !anthropicJSONEqual(string(raw), want) {
		t.Fatalf("replay changed blocks:\ngot %s\nwant %s", raw, want)
	}
}

func TestAnthropicReasoningReplayGuards(t *testing.T) {
	var response AnthropicResponse
	if err := json.Unmarshal([]byte(`{"content":`+anthropicSignedBlocks+`}`), &response); err != nil {
		t.Fatal(err)
	}
	original := convertAnthropicResponse(response, "claude-test").Choices[0].Message
	for _, test := range []struct {
		name  string
		edit  func(*Message)
		model string
	}{
		{"provider", func(m *Message) { m.ProviderState.Provider = "gemini" }, "claude-test"},
		{"model", func(m *Message) {}, "other-model"},
		{"unknown-model", func(m *Message) {}, ""},
		{"old-history", func(m *Message) { m.ProviderState = nil }, "claude-test"},
		{"edited-text", func(m *Message) { m.Content = "edited" }, "claude-test"},
		{"edited-reasoning", func(m *Message) { m.ReasoningContent = "edited" }, "claude-test"},
		{"edited-tool", func(m *Message) { m.ToolCalls[0].Function.Arguments = `{"n":2}` }, "claude-test"},
		{"user-role", func(m *Message) { m.Role = "user" }, "claude-test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, _ := json.Marshal(original)
			var msg Message
			_ = json.Unmarshal(data, &msg)
			test.edit(&msg)
			req, err := buildAnthropicRequest(ChatRequest{Model: test.model, Messages: []Message{msg}})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(req.Messages)
			for _, forbidden := range []string{"signature", "redacted_thinking", "private reasoning", "encrypted-value"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatalf("stale/private state replayed: %s", raw)
				}
			}
			if test.name == "edited-text" && !strings.Contains(string(raw), "edited") {
				t.Fatal("edited text lost")
			}
		})
	}
	// Formatting-only argument changes are safe.
	original.ToolCalls[0].Function.Arguments = `{ "n" : 1 }`
	assertAnthropicReplay(t, original, anthropicSignedBlocks)
}

func TestAnthropicReasoningReplaysSuppressedToolCommentary(t *testing.T) {
	var raw AnthropicResponse
	if err := json.Unmarshal([]byte(`{"content":`+anthropicSignedBlocks+`}`), &raw); err != nil {
		t.Fatal(err)
	}
	msg := convertAnthropicResponse(raw, "claude-test").Choices[0].Message
	msg.Content = "" // NapCat stores tool commentary as protocol state, not delivered text.
	assertAnthropicReplay(t, msg, anthropicSignedBlocks)
}

func TestAnthropicReasoningStreamReplay(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"role":"assistant","model":"claude-test-20251001"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":"","future":{"opaque":123}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-value"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"encrypted-value","future_flag":true}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":"","citations":[]}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"answer"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"tool_1","name":"lookup","input":{},"extension":"keep"}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"n\":"}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		`{"type":"message_stop"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer server.Close()
	msg := Message{Role: "assistant"}
	var last StreamChunk
	err := NewAnthropicClient(server.URL, "test").ChatStream(context.Background(), ChatRequest{Model: "claude-test"}, func(chunk StreamChunk) error {
		last = chunk
		for _, choice := range chunk.Choices {
			d := choice.Delta
			msg.Content += d.Content
			msg.ReasoningContent += d.ReasoningContent
			if d.ProviderState != nil {
				msg.ProviderState = d.ProviderState
			}
			for _, tc := range d.ToolCalls {
				if len(msg.ToolCalls) == 0 {
					msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: tc.ID, Type: tc.Type, Function: ToolCallFunction{Name: tc.Function.Name}})
				}
				msg.ToolCalls[0].Function.Arguments += tc.Function.Arguments
			}
			if choice.FinishReason != "" && d.ProviderState == nil {
				t.Error("finish chunk missing complete state")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if last.Choices[0].Delta.ProviderState == nil {
		t.Fatal("message_stop missing final state")
	}
	assertAnthropicReplay(t, msg, anthropicSignedBlocks)
}
