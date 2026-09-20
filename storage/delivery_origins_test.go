package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func originReminder(t *testing.T, m *ReminderManager) Reminder {
	t.Helper()
	now := time.Now().UTC()
	r, err := m.Create(Reminder{ID: "reminder", Channel: ReminderChannelClawBot, SessionID: "session", PromptID: "prompt", Target: ReminderTarget{Kind: ReminderTargetKindUser, UserID: "user"}, Title: "title", ReminderPrompt: "prompt", DueAt: now, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return *r
}

func TestReminderDeliveryRecoveryAndInvalidation(t *testing.T) {
	for _, bindBeforeRestart := range []bool{false, true} {
		t.Run(map[bool]string{false: "unbound", true: "bound"}[bindBeforeRestart], func(t *testing.T) {
			dir := t.TempDir()
			m := NewReminderManager(dir)
			snapshot := originReminder(t, m)
			if _, ok, err := m.TryMarkFiring(snapshot.ID, time.Now()); err != nil || !ok {
				t.Fatalf("firing: %v %v", ok, err)
			}
			if bindBeforeRestart {
				if ok, err := m.BindDelivery(snapshot, "delivery", time.Now()); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			m = NewReminderManager(dir)
			if bindBeforeRestart {
				r, _ := m.Get(snapshot.ID)
				if r.Status != ReminderStatusFiring {
					t.Fatal("lost firing binding")
				}
			}
			if ok, err := m.BindDelivery(snapshot, "delivery", time.Now()); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if _, err := m.CancelPending(snapshot.ID, time.Now()); err != nil {
				t.Fatal(err)
			}
			if ok, err := m.BindDelivery(snapshot, "delivery", time.Now()); err != nil || ok {
				t.Fatal(ok, err)
			}
			if err := m.CompleteDelivery(snapshot, "delivery", false, time.Now()); err != nil {
				t.Fatal(err)
			}
			if _, err := m.MarkSent(snapshot.ID, time.Now()); err != nil {
				t.Fatal(err)
			}
			r, _ := m.Get(snapshot.ID)
			if r.Status != ReminderStatusCancelled {
				t.Fatal(r.Status)
			}
		})
	}
}

func TestReminderDeliveryEditAndSaveFailure(t *testing.T) {
	m := NewReminderManager(t.TempDir())
	snapshot := originReminder(t, m)
	if ok, err := m.BindDelivery(snapshot, "delivery", time.Now()); err != nil || !ok {
		t.Fatal(ok, err)
	}
	title := "new title"
	edited, err := m.UpdatePending(snapshot.ID, ReminderPatch{Title: &title}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision == snapshot.Revision {
		t.Fatal("revision unchanged")
	}
	if ok, err := m.BindDelivery(snapshot, "delivery", time.Now()); err != nil || ok {
		t.Fatal(ok, err)
	}
	if err := m.CompleteDelivery(snapshot, "delivery", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	r, _ := m.Get(snapshot.ID)
	if r.Status != ReminderStatusPending {
		t.Fatal(r.Status)
	}
	m.filePath = filepath.Join(t.TempDir(), "missing", "reminders.json")
	if _, err := m.CancelPending(snapshot.ID, time.Now()); err == nil {
		t.Fatal("expected write error")
	}
	r, _ = m.Get(snapshot.ID)
	if r.Status != ReminderStatusPending || r.Revision != edited.Revision {
		t.Fatal("failed write published")
	}
	if ok, err := m.BindDelivery(*edited, "next", time.Now()); err == nil || ok {
		t.Fatal("expected bind write error")
	}
	r, _ = m.Get(snapshot.ID)
	if r.DeliveryID != "" {
		t.Fatal("failed binding published")
	}
}

func TestGreetingDeliveryRecoveryAndReplacement(t *testing.T) {
	dir := t.TempDir()
	m := NewIdleGreetingManager(dir)
	now := time.Now().UTC()
	task, err := m.UpsertPending(IdleGreetingTask{ID: "greeting", Channel: ReminderChannelClawBot, SessionID: "session", Target: ReminderTarget{Kind: ReminderTargetKindUser, UserID: "user"}, DueAt: now, LastUserAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.TryMarkFiring(task.ID, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	m = NewIdleGreetingManager(dir)
	if ok, err := m.BindDelivery(*task, "delivery", now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	m = NewIdleGreetingManager(dir)
	current, _ := m.Get(task.ID)
	if current.Status != IdleGreetingStatusFiring {
		t.Fatal(current.Status)
	}
	replacement := *task
	replacement.LastUserAt = now
	newer, err := m.UpsertPending(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := m.BindDelivery(*task, "delivery", now); err != nil || ok {
		t.Fatal(ok, err)
	}
	if err := m.CompleteDelivery(*task, "delivery"); err != nil {
		t.Fatal(err)
	}
	current, ok := m.Get(task.ID)
	if !ok || current.Revision != newer.Revision {
		t.Fatal("completion deleted replacement")
	}
	if err := m.Delete(task.ID); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if ok, err := m.BindDelivery(*newer, "next", now); err != nil || ok {
		t.Fatal(ok, err)
	}
}
