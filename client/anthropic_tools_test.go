package client

import "testing"

func TestBuildAnthropicMessages_EncodesToolUseAndToolResult(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "sys"},
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
			Content:    `{"ok":true,"tool":"send_pat","data":{"name":"a","target":"b"},"error":""}`,
		},
		{Role: "assistant", Content: "done"},
	}

	anthropicMessages, system, err := buildAnthropicMessages(messages)
	if err != nil {
		t.Fatalf("buildAnthropicMessages err: %v", err)
	}
	if len(system) == 0 {
		t.Fatalf("expected system instruction")
	}
	if len(anthropicMessages) < 4 {
		t.Fatalf("expected >= 4 messages, got %d", len(anthropicMessages))
	}

	foundToolUse := false
	for _, block := range anthropicMessages[1].Content { // assistant tool_use message (system removed, user at 0)
		if block.Type != "tool_use" {
			continue
		}
		if block.ID != "call_1" {
			t.Fatalf("tool_use id=%q, want call_1", block.ID)
		}
		if block.Name != "send_pat" {
			t.Fatalf("tool_use name=%q, want send_pat", block.Name)
		}
		foundToolUse = true
	}
	if !foundToolUse {
		t.Fatalf("expected tool_use block in assistant message")
	}

	toolResultMsg := anthropicMessages[2]
	if toolResultMsg.Role != "user" {
		t.Fatalf("tool_result role=%q, want user", toolResultMsg.Role)
	}
	if len(toolResultMsg.Content) == 0 || toolResultMsg.Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result content block")
	}
	if toolResultMsg.Content[0].ToolUseID != "call_1" {
		t.Fatalf("tool_result tool_use_id=%q, want call_1", toolResultMsg.Content[0].ToolUseID)
	}
}
