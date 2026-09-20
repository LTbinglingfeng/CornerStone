package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"testing"
	"time"
)

func TestDeliveryOriginGreetingValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "completed-input", "disabled", "window", "late", "history", "inbox", "merged-inbox", "deleted", "replaced"} {
		t.Run(scenario, func(t *testing.T) {
			h, cfg, chats, service, manager, clock := newIdleGreetingTestHarness(t, "http://example.invalid")
			if _, err := chats.CreateSession("session", "Alice", "prompt", "Alice"); err != nil {
				t.Fatal(err)
			}
			task, err := manager.UpsertPending(storage.IdleGreetingTask{ID: "greeting", Channel: storage.ReminderChannelClawBot, SessionID: "session", Target: storage.ReminderTarget{Kind: storage.ReminderTargetKindUser, UserID: "user"}, DueAt: clock.now, LastUserAt: clock.now.Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			s := &ClawBotService{handler: h}
			job := storage.ClawBotDeliveryJob{ID: "delivery", Kind: "greeting", SessionID: "session", UserID: "user", Greeting: task}
			switch scenario {
			case "disabled":
				c := cfg.Get()
				c.IdleGreeting.Enabled = false
				if err := cfg.Update(c); err != nil {
					t.Fatal(err)
				}
			case "window":
				c := cfg.Get()
				c.IdleGreeting.TimeWindows = []config.IdleGreetingTimeWindow{{Start: "09:00", End: "10:00"}}
				if err := cfg.Update(c); err != nil {
					t.Fatal(err)
				}
			case "late":
				clock.now = clock.now.Add(service.maxFireLag + time.Second)
			case "history":
				if err := chats.AddMessages("session", []storage.ChatMessage{{Role: "user", Content: "hello", Timestamp: clock.now}}); err != nil {
					t.Fatal(err)
				}
			case "completed-input":
				s.state.Delivery = &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{{ID: "trigger", Kind: "chat", Status: "done", UserID: "user", CreatedAt: task.LastUserAt, UpdatedAt: clock.now, InputTimes: []time.Time{task.LastUserAt}, Inputs: []client.ClawBotIncomingMessage{{FromUserID: "user"}}, InputMessages: []storage.ChatMessage{{Role: "user", Content: "trigger", Timestamp: task.LastUserAt}}}}}
			case "inbox", "merged-inbox":
				created := clock.now
				if scenario == "merged-inbox" {
					created = task.LastUserAt.Add(-time.Minute)
				}
				s.state.Delivery = &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{{ID: "incoming", Kind: "chat", UserID: "user", CreatedAt: created, UpdatedAt: clock.now, InputTimes: []time.Time{clock.now}, Inputs: []client.ClawBotIncomingMessage{{FromUserID: "user"}}}}}
			case "deleted":
				if err := manager.Delete(task.ID); err != nil {
					t.Fatal(err)
				}
			case "replaced":
				next := *task
				next.LastUserAt = clock.now
				if _, err := manager.UpsertPending(next); err != nil {
					t.Fatal(err)
				}
			}
			valid, err := s.validateDeliveryOrigin(job)
			if err != nil {
				t.Fatal(err)
			}
			if valid != (scenario == "valid" || scenario == "completed-input") {
				t.Fatalf("valid=%v", valid)
			}
			if err := s.finishDeliveryOrigin(job, !valid); err != nil {
				t.Fatal(err)
			}
			_, exists := manager.Get(task.ID)
			if exists != (scenario == "replaced") {
				t.Fatalf("source exists=%v", exists)
			}
		})
	}
}

func TestDeliveryOriginReminderCancelledOrEdited(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "edit"}[edit], func(t *testing.T) {
			manager := storage.NewReminderManager(t.TempDir())
			now := time.Now()
			task, err := manager.Create(storage.Reminder{ID: "reminder", Channel: storage.ReminderChannelClawBot, SessionID: "session", PromptID: "prompt", Target: storage.ReminderTarget{Kind: storage.ReminderTargetKindUser, UserID: "user"}, Title: "title", ReminderPrompt: "prompt", DueAt: now})
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{}
			h.reminderService = NewReminderService(h, manager, &stubExactTimeService{now: now})
			s := &ClawBotService{handler: h}
			job := storage.ClawBotDeliveryJob{ID: "delivery", Kind: "reminder", Reminder: task}
			if ok, err := s.validateDeliveryOrigin(job); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if edit {
				title := "edited"
				_, err = manager.UpdatePending(task.ID, storage.ReminderPatch{Title: &title}, now)
			} else {
				_, err = manager.CancelPending(task.ID, now)
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := s.validateDeliveryOrigin(job); err != nil || ok {
				t.Fatal(ok, err)
			}
			if err := s.finishDeliveryOrigin(job, false); err != nil {
				t.Fatal(err)
			}
			current, _ := manager.Get(task.ID)
			if current.Status == storage.ReminderStatusSent {
				t.Fatal("stale completion overwrote producer")
			}
		})
	}
}

func TestDeliveryOriginUnavailableIsBlocked(t *testing.T) {
	s := &ClawBotService{handler: &Handler{}}
	for _, kind := range []string{"reminder", "greeting"} {
		if valid, err := s.validateDeliveryOrigin(storage.ClawBotDeliveryJob{Kind: kind}); valid || err == nil {
			t.Fatalf("missing %s dependencies must block: valid=%v err=%v", kind, valid, err)
		}
	}
}
