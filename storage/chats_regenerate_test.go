package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestChatManager(t *testing.T) *ChatManager {
	t.Helper()
	dir := t.TempDir()
	return NewChatManager(dir)
}

func addTestMessages(t *testing.T, cm *ChatManager, sid string, roles ...string) {
	t.Helper()
	for _, role := range roles {
		if err := cm.AddMessage(sid, role, role+"_msg"); err != nil {
			t.Fatalf("AddMessage(%s) failed: %v", role, err)
		}
	}
}

func TestDeleteTrailingAssistantBatch_SingleAssistant(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-single"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "assistant")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	record, ok := cm.GetSession(sid)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 1 {
		t.Fatalf("expected 1 message remaining, got %d", len(record.Messages))
	}
	if record.Messages[0].Role != "user" {
		t.Fatalf("expected remaining message to be user, got %s", record.Messages[0].Role)
	}
}

func TestDeleteTrailingAssistantBatch_MultipleAssistant(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-multi"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "assistant", "assistant", "assistant")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted, got %d", deleted)
	}

	record, _ := cm.GetSession(sid)
	if len(record.Messages) != 1 {
		t.Fatalf("expected 1 message remaining, got %d", len(record.Messages))
	}
}

func TestDeleteTrailingAssistantBatch_TailIsUser(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-tail-user"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "assistant", "user")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted when tail is user, got %d", deleted)
	}

	record, _ := cm.GetSession(sid)
	if len(record.Messages) != 3 {
		t.Fatalf("expected 3 messages unchanged, got %d", len(record.Messages))
	}
}

func TestDeleteTrailingAssistantBatch_NoAssistant(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-no-asst"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "user")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted, got %d", deleted)
	}
}

func TestDeleteTrailingAssistantBatch_EmptySession(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-empty"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted for empty session, got %d", deleted)
	}
}

func TestDeleteTrailingAssistantBatch_MixedConversation(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-mixed"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	// user, assistant, user, assistant, assistant
	addTestMessages(t, cm, sid, "user", "assistant", "user", "assistant", "assistant")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	record, _ := cm.GetSession(sid)
	if len(record.Messages) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(record.Messages))
	}
	// remaining: user, assistant, user
	expected := []string{"user", "assistant", "user"}
	for i, exp := range expected {
		if record.Messages[i].Role != exp {
			t.Fatalf("message[%d] expected role %s, got %s", i, exp, record.Messages[i].Role)
		}
	}
}

func TestDeleteTrailingAssistantBatch_RemovesTrailingNonUserBatch(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-trailing-non-user"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	err := cm.AddMessages(sid, []ChatMessage{
		{Role: "user", Content: "user_msg", Timestamp: now},
		{Role: "assistant", Content: "assistant_msg", Timestamp: now.Add(time.Millisecond)},
		{Role: "tool", Content: "tool_msg", Timestamp: now.Add(2 * time.Millisecond)},
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	record, _ := cm.GetSession(sid)
	if len(record.Messages) != 1 {
		t.Fatalf("expected 1 message remaining, got %d", len(record.Messages))
	}
	if record.Messages[0].Role != "user" {
		t.Fatalf("expected remaining message to be user, got %s", record.Messages[0].Role)
	}
}

func TestDeleteTrailingResponseBatch_NoUserDeletesEntireTail(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-no-user"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	err := cm.AddMessages(sid, []ChatMessage{
		{Role: "assistant", Content: "assistant_msg", Timestamp: now},
		{Role: "tool", Content: "tool_msg", Timestamp: now.Add(time.Millisecond)},
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := cm.DeleteTrailingResponseBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	record, _ := cm.GetSession(sid)
	if len(record.Messages) != 0 {
		t.Fatalf("expected empty session, got %d messages", len(record.Messages))
	}
}

func TestReminderManager_CreateUpdateAndListDuePending(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reminders")
	manager := NewReminderManager(dir)

	dueAt := time.Date(2026, 4, 5, 19, 30, 0, 0, time.FixedZone("CST", 8*3600))
	createdAt := dueAt.Add(-time.Hour)
	reminder, err := manager.Create(Reminder{
		ID:             "reminder-1",
		Channel:        ReminderChannelWeb,
		SessionID:      "session-1",
		PromptID:       "prompt-1",
		PromptName:     "Alice",
		Title:          "喝水提醒",
		ReminderPrompt: "到点后提醒用户喝水",
		DueAt:          dueAt,
		Status:         ReminderStatusPending,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("Create reminder failed: %v", err)
	}
	if reminder.Status != ReminderStatusPending {
		t.Fatalf("status = %q, want %q", reminder.Status, ReminderStatusPending)
	}

	newTitle := "新的喝水提醒"
	newPrompt := "到点后温柔地提醒用户喝水"
	newDueAt := dueAt.Add(30 * time.Minute)
	updated, err := manager.UpdatePending(reminder.ID, ReminderPatch{
		Title:          &newTitle,
		ReminderPrompt: &newPrompt,
		DueAt:          &newDueAt,
	}, createdAt.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("UpdatePending failed: %v", err)
	}
	if updated.Title != newTitle {
		t.Fatalf("title = %q, want %q", updated.Title, newTitle)
	}
	if updated.ReminderPrompt != newPrompt {
		t.Fatalf("reminder_prompt = %q, want %q", updated.ReminderPrompt, newPrompt)
	}

	notDue := manager.ListDuePending(newDueAt.Add(-time.Second))
	if len(notDue) != 0 {
		t.Fatalf("ListDuePending before due returned %d items, want 0", len(notDue))
	}

	due := manager.ListDuePending(newDueAt)
	if len(due) != 1 || due[0].ID != reminder.ID {
		t.Fatalf("ListDuePending at due = %#v, want reminder-1", due)
	}
}

func TestReminderManager_ReloadResetsFiringToPending(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reminders")
	manager := NewReminderManager(dir)

	dueAt := time.Date(2026, 4, 5, 19, 30, 0, 0, time.UTC)
	createdAt := dueAt.Add(-time.Hour)
	reminder, err := manager.Create(Reminder{
		ID:             "reminder-1",
		Channel:        ReminderChannelClawBot,
		SessionID:      "session-1",
		PromptID:       "prompt-1",
		PromptName:     "Alice",
		ClawBotUserID:  "wx-user",
		Title:          "喝水提醒",
		ReminderPrompt: "到点后提醒用户喝水",
		DueAt:          dueAt,
		Status:         ReminderStatusPending,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("Create reminder failed: %v", err)
	}

	_, transitioned, err := manager.TryMarkFiring(reminder.ID, dueAt)
	if err != nil {
		t.Fatalf("TryMarkFiring failed: %v", err)
	}
	if !transitioned {
		t.Fatal("TryMarkFiring did not transition reminder to firing")
	}

	reloaded := NewReminderManager(dir)
	loaded, ok := reloaded.Get(reminder.ID)
	if !ok || loaded == nil {
		t.Fatal("reloaded reminder not found")
	}
	if loaded.Status != ReminderStatusPending {
		t.Fatalf("reloaded status = %q, want %q", loaded.Status, ReminderStatusPending)
	}
	if loaded.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", loaded.Attempts)
	}
}

func TestSnapshotTrailingResponseBatch_TrimmedSessionAndTail(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-snapshot-tail"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := cm.AddMessages(sid, []ChatMessage{
		{Role: "user", Content: "hello", Timestamp: now},
		{Role: "assistant", Content: "old reply", Timestamp: now.Add(time.Millisecond)},
		{Role: "tool", Content: `{"ok":true}`, ToolCallID: "call-1", Timestamp: now.Add(2 * time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := cm.SnapshotTrailingResponseBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Session == nil {
		t.Fatal("snapshot session should not be nil")
	}
	if len(snapshot.Session.Messages) != 1 {
		t.Fatalf("trimmed session message count = %d, want 1", len(snapshot.Session.Messages))
	}
	if snapshot.Session.Messages[0].Role != "user" || snapshot.Session.Messages[0].Content != "hello" {
		t.Fatalf("trimmed session first message = %#v, want user hello", snapshot.Session.Messages[0])
	}
	if len(snapshot.TailMessages) != 2 {
		t.Fatalf("tail message count = %d, want 2", len(snapshot.TailMessages))
	}
	if snapshot.TailMessages[0].Role != "assistant" || snapshot.TailMessages[1].Role != "tool" {
		t.Fatalf("tail roles = [%s %s], want [assistant tool]", snapshot.TailMessages[0].Role, snapshot.TailMessages[1].Role)
	}
}

func TestReplaceTrailingResponseBatch_ReplacesTailAtomically(t *testing.T) {
	dir := t.TempDir()
	cm := NewChatManager(dir)
	sid := "test-replace-tail"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := cm.AddMessages(sid, []ChatMessage{
		{Role: "user", Content: "hello", Timestamp: now},
		{Role: "assistant", Content: "old reply", Timestamp: now.Add(time.Millisecond)},
		{Role: "tool", Content: `{"ok":true}`, ToolCallID: "call-1", Timestamp: now.Add(2 * time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := cm.SnapshotTrailingResponseBatch(sid)
	if err != nil {
		t.Fatal(err)
	}

	replacement := []ChatMessage{
		{Role: "assistant", Content: "fresh reply", Timestamp: now.Add(3 * time.Millisecond)},
	}
	if err := cm.ReplaceTrailingResponseBatch(sid, snapshot.TailMessages, replacement); err != nil {
		t.Fatal(err)
	}

	record, ok := cm.GetSession(sid)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(record.Messages))
	}
	if record.Messages[0].Role != "user" || record.Messages[0].Content != "hello" {
		t.Fatalf("first message = %#v, want user hello", record.Messages[0])
	}
	if record.Messages[1].Role != "assistant" || record.Messages[1].Content != "fresh reply" {
		t.Fatalf("second message = %#v, want assistant fresh reply", record.Messages[1])
	}

	cm2 := NewChatManager(dir)
	record, ok = cm2.GetSession(sid)
	if !ok {
		t.Fatal("session not found after reload")
	}
	if len(record.Messages) != 2 || record.Messages[1].Content != "fresh reply" {
		t.Fatalf("reloaded messages = %#v, want fresh reply persisted", record.Messages)
	}
}

func TestReplaceTrailingResponseBatch_ConflictKeepsHistory(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-replace-tail-conflict"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := cm.AddMessages(sid, []ChatMessage{
		{Role: "user", Content: "hello", Timestamp: now},
		{Role: "assistant", Content: "old reply", Timestamp: now.Add(time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := cm.SnapshotTrailingResponseBatch(sid)
	if err != nil {
		t.Fatal(err)
	}

	if err := cm.AddMessage(sid, "user", "later"); err != nil {
		t.Fatal(err)
	}

	err = cm.ReplaceTrailingResponseBatch(sid, snapshot.TailMessages, []ChatMessage{
		{Role: "assistant", Content: "fresh reply", Timestamp: now.Add(2 * time.Millisecond)},
	})
	if err != ErrTrailingResponseBatchChanged {
		t.Fatalf("replace err = %v, want %v", err, ErrTrailingResponseBatchChanged)
	}

	record, ok := cm.GetSession(sid)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(record.Messages))
	}
	if record.Messages[1].Content != "old reply" {
		t.Fatalf("assistant content = %q, want old reply", record.Messages[1].Content)
	}
	if record.Messages[2].Role != "user" || record.Messages[2].Content != "later" {
		t.Fatalf("last message = %#v, want user later", record.Messages[2])
	}
}

func TestDeleteTrailingAssistantBatch_NonExistentSession(t *testing.T) {
	cm := newTestChatManager(t)
	_, err := cm.DeleteTrailingAssistantBatch("nonexistent")
	if err != os.ErrNotExist {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestDeleteTrailingAssistantBatch_Persistence(t *testing.T) {
	dir := t.TempDir()
	cm := NewChatManager(dir)
	sid := "test-persist"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "assistant", "assistant")

	deleted, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	// 重新加载验证持久化
	cm2 := NewChatManager(dir)
	record, ok := cm2.GetSession(sid)
	if !ok {
		t.Fatal("session not found after reload")
	}
	if len(record.Messages) != 1 {
		t.Fatalf("expected 1 message after reload, got %d", len(record.Messages))
	}
}

// 确保 UpdatedAt 被更新
func TestDeleteTrailingAssistantBatch_UpdatesTimestamp(t *testing.T) {
	cm := newTestChatManager(t)
	sid := "test-timestamp"
	if _, err := cm.CreateSession(sid, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	addTestMessages(t, cm, sid, "user", "assistant")

	before, _ := cm.GetSession(sid)
	beforeUpdatedAt := before.UpdatedAt

	time.Sleep(time.Millisecond)

	_, err := cm.DeleteTrailingAssistantBatch(sid)
	if err != nil {
		t.Fatal(err)
	}

	after, _ := cm.GetSession(sid)
	if !after.UpdatedAt.After(beforeUpdatedAt) {
		t.Fatal("UpdatedAt should have been updated")
	}
}
