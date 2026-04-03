package storage

import (
	"cornerstone/client"
	"testing"
	"time"
)

func TestChatToolCallID_PersistsAndRegenerateDeletesTailBatch(t *testing.T) {
	dir := t.TempDir()
	cm := NewChatManager(dir)
	sid := "test-tool-call-id"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	messages := []ChatMessage{
		{Role: "user", Content: "user", Timestamp: now},
		{
			Role:      "assistant",
			Content:   "",
			Timestamp: now.Add(time.Millisecond),
			ToolCalls: []client.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: client.ToolCallFunction{
						Name:      "send_pat",
						Arguments: `{"name":"a","target":"b"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			Content:    `{"ok":true,"tool":"send_pat","data":{"name":"a","target":"b"},"error":""}`,
			ToolCallID: "call_1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
		{Role: "assistant", Content: "done", Timestamp: now.Add(3 * time.Millisecond)},
	}
	if err := cm.AddMessages(sid, messages); err != nil {
		t.Fatal(err)
	}

	// Verify persistence keeps tool_call_id
	cm2 := NewChatManager(dir)
	record, ok := cm2.GetSession(sid)
	if !ok {
		t.Fatal("session not found after reload")
	}
	foundTool := false
	for _, msg := range record.Messages {
		if msg.Role == "tool" {
			foundTool = true
			if msg.ToolCallID != "call_1" {
				t.Fatalf("tool_call_id = %q, want %q", msg.ToolCallID, "call_1")
			}
		}
	}
	if !foundTool {
		t.Fatal("tool message not found")
	}

	// Regenerate semantics: delete tail response batch after last user (assistant/tool/assistant).
	deleted, err := cm2.DeleteTrailingResponseBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
}
