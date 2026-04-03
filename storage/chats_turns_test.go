package storage

import (
	"cornerstone/client"
	"testing"
	"time"
)

func addTimedMessages(t *testing.T, cm *ChatManager, sid string, messages []ChatMessage) {
	t.Helper()
	if err := cm.AddMessages(sid, messages); err != nil {
		t.Fatalf("AddMessages failed: %v", err)
	}
}

func TestGetRecentTurns_IncludesWholeResponseBatch(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-recent-turns-batch"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	addTimedMessages(t, cm, sid, []ChatMessage{
		{Role: "user", Content: "u1", Timestamp: now},
		{Role: "assistant", Content: "a1", Timestamp: now.Add(time.Millisecond)},
		{Role: "user", Content: "u2", Timestamp: now.Add(2 * time.Millisecond)},
		{
			Role:      "assistant",
			Content:   "",
			Timestamp: now.Add(3 * time.Millisecond),
			ToolCalls: []client.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: client.ToolCallFunction{
						Name:      "send_pat",
						Arguments: `{"target":"Bob"}`,
					},
				},
			},
		},
		{Role: "tool", Content: `{"ok":true}`, ToolCallID: "call_1", Timestamp: now.Add(4 * time.Millisecond)},
		{Role: "assistant", Content: "a2", Timestamp: now.Add(5 * time.Millisecond)},
		{Role: "user", Content: "u3", Timestamp: now.Add(6 * time.Millisecond)},
		{Role: "assistant", Content: "a3", Timestamp: now.Add(7 * time.Millisecond)},
	})

	got := cm.GetRecentTurns(sid, 2)
	wantRoles := []string{"user", "assistant", "tool", "assistant", "user", "assistant"}
	if len(got) != len(wantRoles) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(wantRoles))
	}
	for i, want := range wantRoles {
		if got[i].Role != want {
			t.Fatalf("message[%d].Role = %q, want %q", i, got[i].Role, want)
		}
	}
	if got[0].Content != "u2" {
		t.Fatalf("first turn should start at u2, got %q", got[0].Content)
	}
}

func TestGetRecentTurns_GroupsConsecutiveUserMessages(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-recent-turns-consecutive-users"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	addTimedMessages(t, cm, sid, []ChatMessage{
		{Role: "user", Content: "u1", Timestamp: now},
		{Role: "user", Content: "u1b", Timestamp: now.Add(time.Millisecond)},
		{Role: "assistant", Content: "a1", Timestamp: now.Add(2 * time.Millisecond)},
		{Role: "user", Content: "u2", Timestamp: now.Add(3 * time.Millisecond)},
		{Role: "assistant", Content: "a2", Timestamp: now.Add(4 * time.Millisecond)},
		{Role: "user", Content: "u3", Timestamp: now.Add(5 * time.Millisecond)},
		{Role: "user", Content: "u3b", Timestamp: now.Add(6 * time.Millisecond)},
		{Role: "assistant", Content: "a3", Timestamp: now.Add(7 * time.Millisecond)},
	})

	got := cm.GetRecentTurns(sid, 1)
	wantContents := []string{"u3", "u3b", "a3"}
	if len(got) != len(wantContents) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(wantContents))
	}
	for i, want := range wantContents {
		if got[i].Content != want {
			t.Fatalf("message[%d].Content = %q, want %q", i, got[i].Content, want)
		}
	}
}

func TestGetRecentTurns_ReturnsAllWhenLimitExceedsHistory(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-recent-turns-all"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	addTimedMessages(t, cm, sid, []ChatMessage{
		{Role: "assistant", Content: "banner", Timestamp: now},
		{Role: "user", Content: "u1", Timestamp: now.Add(time.Millisecond)},
		{Role: "assistant", Content: "a1", Timestamp: now.Add(2 * time.Millisecond)},
	})

	got := cm.GetRecentTurns(sid, 5)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].Content != "banner" {
		t.Fatalf("got[0].Content = %q, want banner", got[0].Content)
	}
}
