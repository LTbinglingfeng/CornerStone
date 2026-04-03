package client

import "testing"

func TestBuildOpenAIRequest_EncodesToolCallID(t *testing.T) {
	req := ChatRequest{
		Model: "gpt-test",
		Messages: []Message{
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
				Content:    `{"ok":true,"tool":"send_pat","data":{"name":"a","target":"b"},"error":""}`,
			},
		},
	}

	built, err := buildOpenAIRequest(req)
	if err != nil {
		t.Fatalf("buildOpenAIRequest err: %v", err)
	}
	if len(built.Messages) != 2 {
		t.Fatalf("messages len=%d, want 2", len(built.Messages))
	}
	if built.Messages[1].Role != "tool" {
		t.Fatalf("role=%q, want tool", built.Messages[1].Role)
	}
	if built.Messages[1].ToolCallID != "call_1" {
		t.Fatalf("tool_call_id=%q, want call_1", built.Messages[1].ToolCallID)
	}
}
