package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildOpenAIResponsesRequest_EncodesToolMessages(t *testing.T) {
	toolOutput := `{"ok":true,"tool":"send_pat","data":{"name":"a","target":"b"},"error":""}`
	req := ChatRequest{
		Model: "gpt-test",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "send_pat",
							Arguments: `{"name":"a","target":"b"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				Content:    toolOutput,
			},
			{Role: "assistant", Content: "done"},
		},
	}

	built, err := buildOpenAIResponsesRequest(req)
	if err != nil {
		t.Fatalf("buildOpenAIResponsesRequest err: %v", err)
	}

	items, ok := built.Input.([]interface{})
	if !ok {
		t.Fatalf("expected input items slice, got %T", built.Input)
	}

	foundCall := false
	foundOutput := false
	for _, item := range items {
		switch v := item.(type) {
		case openAIResponsesFunctionCallItem:
			if v.CallID == "call_1" && v.Name == "send_pat" {
				foundCall = true
			}
		case openAIResponsesFunctionCallOutputItem:
			if v.CallID == "call_1" {
				foundOutput = true
				if v.Output != toolOutput {
					t.Fatalf("tool output = %q, want %q", v.Output, toolOutput)
				}
			}
		}
	}
	if !foundCall {
		t.Fatalf("expected function_call item")
	}
	if !foundOutput {
		t.Fatalf("expected function_call_output item")
	}
}

func TestOpenAIResponsesClientChat_ParsesSSEBody(t *testing.T) {
	sse := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_test","created_at":123,"model":"gpt-test"}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		"event: response.output_text.done",
		`data: {"type":"response.output_text.done","text":"hello"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_test","created_at":123,"model":"gpt-test","status":"completed"}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := NewResponsesClient(srv.URL, "test")
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("expected non-empty response")
	}
	if got := resp.Choices[0].Message.Content; got != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
	if got := resp.ID; got != "resp_test" {
		t.Fatalf("resp id = %q, want %q", got, "resp_test")
	}
}

func TestOpenAIResponsesClientChat_ParsesSSEBodyEvenWhenContentTypeIsJSON(t *testing.T) {
	sse := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_test","created_at":123,"model":"gpt-test"}}`,
		"",
		"event: response.output_text.done",
		`data: {"type":"response.output_text.done","text":"hello"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := NewResponsesClient(srv.URL, "test")
	resp, err := c.Chat(context.Background(), ChatRequest{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("expected non-empty response")
	}
	if got := resp.Choices[0].Message.Content; got != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
}
