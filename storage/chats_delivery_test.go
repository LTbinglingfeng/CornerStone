package storage

import (
	"bytes"
	"cornerstone/client"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newDeliveryTestManager(t *testing.T) *ChatManager {
	t.Helper()
	cm := NewChatManager(t.TempDir())
	if _, err := cm.CreateSession("delivery", "title", "prompt", "name"); err != nil {
		t.Fatal(err)
	}
	return cm
}

func TestDeliveryRestartAndMetadata(t *testing.T) {
	cm := newDeliveryTestManager(t)
	token := "→"
	messages := []ChatMessage{
		{Role: "assistant", ReasoningContent: "reason", ToolCalls: []client.ToolCall{{ID: "call", Type: "function", Function: client.ToolCallFunction{Name: "search", Arguments: `{"q":"test"}`}}}, Timestamp: time.Now().UTC()},
		{Role: "tool", Content: "result", ToolCallID: "call", Timestamp: time.Now().UTC()},
		{Role: "assistant", Content: "reply", ImagePaths: []string{"image"}, TTSAudioPaths: []string{"audio"}, Quote: &QuotedMessage{Content: "quote", ImagePaths: []string{"quoted-image"}}, AssistantMessageSplitToken: &token, Timestamp: time.Now().UTC()},
	}
	want := cloneChatMessages(messages)
	if err := cm.CommitDeliveryBatch("delivery", "op", messages, false, nil); err != nil {
		t.Fatal(err)
	}
	messages[0].ToolCalls[0].Function.Name = "changed"
	messages[2].ImagePaths[0] = "changed"
	messages[2].TTSAudioPaths[0] = "changed"
	messages[2].Quote.ImagePaths[0] = "changed"
	token = "changed"
	record, _ := cm.GetSession("delivery")
	if !reflect.DeepEqual(record.Messages, want) {
		t.Fatal("input aliases persisted messages")
	}
	record.AppliedOperations[0] = "changed"
	page, _, _, err := cm.GetSessionMessagesPage("delivery", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	page.AppliedOperations[0] = "changed"
	snapshot, err := cm.SnapshotTrailingResponseBatch("delivery")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Session.AppliedOperations[0] = "changed"
	before, _ := cm.GetSession("delivery")
	if before.AppliedOperations[0] != "op" {
		t.Fatal("receipt aliases snapshot")
	}
	path := cm.getSessionFilePath("delivery")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	var meta ChatRecord
	if err := json.Unmarshal(lines[0], &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Messages) != 0 || !reflect.DeepEqual(meta.AppliedOperations, []string{"op"}) {
		t.Fatal("invalid metadata line")
	}
	for _, line := range lines[1:] {
		if bytes.Contains(line, []byte("applied_operations")) {
			t.Fatal("receipt in message")
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode: %v", info.Mode())
		}
	}
	cm = NewChatManager(cm.dataDir)
	if err := cm.CommitDeliveryBatch("delivery", "op", messages, true, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := cm.GetSession("delivery")
	if !equalDeliveryJSON(t, before, after) {
		t.Fatalf("restart/retry changed record: %#v", after)
	}
}

func TestDeliveryReplaceCASAndEmpty(t *testing.T) {
	cm := newDeliveryTestManager(t)
	if err := cm.AddMessage("delivery", "user", "question"); err != nil {
		t.Fatal(err)
	}
	old := []ChatMessage{{Role: "assistant", Content: "old"}}
	if err := cm.CommitDeliveryBatch("delivery", "old", old, false, nil); err != nil {
		t.Fatal(err)
	}
	next := []ChatMessage{{Role: "assistant", Content: "new"}}
	before, _ := cm.GetSession("delivery")
	if err := cm.CommitDeliveryBatch("delivery", "new", next, true, nil); !errors.Is(err, ErrTrailingResponseBatchChanged) {
		t.Fatalf("CAS: %v", err)
	}
	after, _ := cm.GetSession("delivery")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("CAS failure changed memory")
	}
	if err := cm.CommitDeliveryBatch("delivery", "new", next, true, old); err != nil {
		t.Fatal(err)
	}
	cm = NewChatManager(cm.dataDir)
	if err := cm.CommitDeliveryBatch("delivery", "new", next, true, old); err != nil {
		t.Fatal(err)
	}
	after, _ = cm.GetSession("delivery")
	if len(after.Messages) != 2 || after.Messages[1].Content != "new" {
		t.Fatal("replacement failed")
	}
	if err := cm.CommitDeliveryBatch("delivery", "silent", nil, true, next); err != nil {
		t.Fatal(err)
	}
	if err := cm.CommitDeliveryBatch("delivery", "silent-append", nil, false, nil); err != nil {
		t.Fatal(err)
	}
	cm = NewChatManager(cm.dataDir)
	if err := cm.CommitDeliveryBatch("delivery", "silent", next, true, next); err != nil {
		t.Fatal(err)
	}
	after, _ = cm.GetSession("delivery")
	if len(after.Messages) != 1 || len(after.AppliedOperations) != 4 {
		t.Fatal("empty batch receipt lost")
	}
}

func TestDeliveryEditsAndDeletesKeepReceipts(t *testing.T) {
	cm := newDeliveryTestManager(t)
	messages := []ChatMessage{{Role: "assistant", Content: "original"}}
	if err := cm.CommitDeliveryBatch("delivery", "op", messages, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := cm.UpdateMessageContentByIndex("delivery", 0, "edited"); err != nil {
		t.Fatal(err)
	}
	cm = NewChatManager(cm.dataDir)
	if err := cm.CommitDeliveryBatch("delivery", "op", messages, false, nil); err != nil {
		t.Fatal(err)
	}
	record, _ := cm.GetSession("delivery")
	if len(record.Messages) != 1 || record.Messages[0].Content != "edited" {
		t.Fatal("edit overwritten")
	}
	if err := cm.DeleteMessageByIndex("delivery", 0); err != nil {
		t.Fatal(err)
	}
	cm = NewChatManager(cm.dataDir)
	if err := cm.CommitDeliveryBatch("delivery", "op", messages, false, nil); err != nil {
		t.Fatal(err)
	}
	record, _ = cm.GetSession("delivery")
	if len(record.Messages) != 0 || len(record.AppliedOperations) != 1 {
		t.Fatal("deleted message resurrected")
	}
}

func TestDeliveryValidationAndMissingSession(t *testing.T) {
	cm := newDeliveryTestManager(t)
	for _, op := range []string{"", " ", strings.Repeat("x", 257)} {
		if err := cm.CommitDeliveryBatch("delivery", op, nil, false, nil); !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("invalid op: %v", err)
		}
	}
	if err := cm.CommitDeliveryBatch("../bad", "op", nil, false, nil); !errors.Is(err, ErrInvalidID) {
		t.Fatal(err)
	}
	if err := cm.DeleteSession("delivery"); err != nil {
		t.Fatal(err)
	}
	if err := cm.CommitDeliveryBatch("delivery", "op", nil, false, nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, ok := cm.GetSession("delivery"); ok {
		t.Fatal("session recreated")
	}
	entries, err := os.ReadDir(cm.dataDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected files: %v %v", entries, err)
	}
}

func TestDeliverySaveFailureDoesNotPublish(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "append", true: "replace"}[replace], func(t *testing.T) {
			cm := newDeliveryTestManager(t)
			old := []ChatMessage{{Role: "assistant", Content: "old"}}
			if err := cm.CommitDeliveryBatch("delivery", "old", old, false, nil); err != nil {
				t.Fatal(err)
			}
			before, _ := cm.GetSession("delivery")
			path := cm.getSessionFilePath("delivery")
			backup := filepath.Join(t.TempDir(), "original")
			if err := os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			// A directory at the destination forces rename failure even when running as root.
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := cm.CommitDeliveryBatch("delivery", "new", []ChatMessage{{Role: "assistant", Content: "new"}}, replace, old); err == nil {
				t.Fatal("expected failure")
			}
			after, _ := cm.GetSession("delivery")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed save published memory")
			}
			entries, err := os.ReadDir(cm.dataDir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary files leaked: %v %v", entries, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, path); err != nil {
				t.Fatal(err)
			}
			restored, _ := NewChatManager(cm.dataDir).GetSession("delivery")
			if !equalDeliveryJSON(t, before, restored) {
				t.Fatal("original file changed")
			}
		})
	}
}

// Compare persisted values without time.Time's process-local monotonic clock.
func equalDeliveryJSON(t *testing.T, a, b *ChatRecord) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(left, right)
}
