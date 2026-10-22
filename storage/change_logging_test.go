package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A subprocess captures the default logger without changing shared logger state.
func TestBusinessChangeLogs(t *testing.T) {
	if os.Getenv("CORNERSTONE_STORAGE_LOG_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBusinessChangeLogs$")
		cmd.Env = append(os.Environ(), "CORNERSTONE_STORAGE_LOG_CHILD=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		text := string(output)
		for _, event := range []string{"persona.created", "persona.updated", "memory.added", "memory.updated", "memory.deleted", "reminder.created", "reminder.retry_scheduled", "reminder.sent", "idle_greeting.created", "idle_greeting.firing", "idle_greeting.deleted"} {
			if n := strings.Count(text, event+":"); n != 1 {
				t.Errorf("%s count=%d\n%s", event, n, text)
			}
		}
		if n := strings.Count(text, "reminder.firing:"); n != 2 {
			t.Errorf("firing count=%d", n)
		}
		if strings.Contains(text, "PRIVATE_SENTINEL") {
			t.Fatalf("private value leaked: %s", text)
		}
		return
	}
	dir := t.TempDir()
	pm := NewPromptManager(filepath.Join(dir, "prompts"))
	if _, err := pm.Create("persona", "PRIVATE_SENTINEL_NAME", "PRIVATE_SENTINEL_CONTENT", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.Update("persona", "PRIVATE_SENTINEL_NAME", "PRIVATE_SENTINEL_CHANGED", "", ""); err != nil {
		t.Fatal(err)
	}
	// Repeating an unchanged update must not emit a business-change event.
	if _, err := pm.Update("persona", "PRIVATE_SENTINEL_NAME", "PRIVATE_SENTINEL_CHANGED", "", ""); err != nil {
		t.Fatal(err)
	}
	mm := NewMemoryManager(filepath.Join(dir, "memories"))
	if err := mm.Add("persona", Memory{ID: "memory", Content: "PRIVATE_SENTINEL_MEMORY"}); err != nil {
		t.Fatal(err)
	}
	content := "PRIVATE_SENTINEL_PATCH"
	if err := mm.Patch("persona", MemoryPatch{ID: "memory", Content: &content}); err != nil {
		t.Fatal(err)
	}
	if _, err := mm.DeleteBatch("persona", []string{"memory"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rm := NewReminderManager(dir)
	reminder := Reminder{ID: "reminder", Channel: ReminderChannelWeb, SessionID: "session", PromptID: "persona", Title: "PRIVATE_SENTINEL_TITLE", ReminderPrompt: "PRIVATE_SENTINEL_BODY", DueAt: now}
	if _, err := rm.Create(reminder); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := rm.TryMarkFiring("reminder", now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := rm.MarkPending("reminder", now, "PRIVATE_SENTINEL_ERROR"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := rm.TryMarkFiring("reminder", now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := rm.MarkSent("reminder", now); err != nil {
		t.Fatal(err)
	}
	im := NewIdleGreetingManager(dir)
	task, err := im.UpsertPending(IdleGreetingTask{ID: "greeting", Channel: ReminderChannelWeb, SessionID: "session", PromptID: "persona", DueAt: now, LastUserAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := im.TryMarkFiring(task.ID, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := im.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	// A regular file in place of a parent directory deterministically fails saves.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	pm.baseDir = blocker
	if _, err := pm.Create("failed", "PRIVATE_SENTINEL_NAME", "PRIVATE_SENTINEL_BODY", "", ""); err == nil {
		t.Fatal("expected persona save failure")
	}
	mm.baseDir = blocker
	if err := mm.Add("persona", Memory{ID: "failed", Content: "PRIVATE_SENTINEL_BODY"}); err == nil {
		t.Fatal("expected memory save failure")
	}
	rm.filePath = filepath.Join(blocker, "reminders.json")
	reminder.ID = "failed"
	if _, err := rm.Create(reminder); err == nil {
		t.Fatal("expected reminder save failure")
	}
	im.filePath = filepath.Join(blocker, "greetings.json")
	task.ID = "failed"
	if _, err := im.UpsertPending(*task); err == nil {
		t.Fatal("expected greeting save failure")
	}
}
