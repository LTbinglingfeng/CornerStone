package client

import (
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
