package api

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"cornerstone/storage"
)

// The first POST is confirmed; the second consumes the shared delivery deadline.
// Embedding the ordinary fake supplies the unused connection methods.
type deadlineQQOfficial struct {
	*fakeQQOfficial
	calls          int
	beforeDeadline func()
}

func (f *deadlineQQOfficial) SendText(ctx context.Context, user, id string, seq int, text string) (string, error) {
	f.calls++
	if f.calls == 1 {
		return f.fakeQQOfficial.SendText(ctx, user, id, seq, text)
	}
	if f.beforeDeadline != nil {
		f.beforeDeadline()
	}
	<-ctx.Done()
	return "", ctx.Err()
}

func TestQQOfficialDeadlineCommitsOnlyConfirmedPrefix(t *testing.T) {
	for _, change := range []string{"none", "disabled", "revoked", "generation", "closed", "edited", "deleted"} {
		t.Run(change, func(t *testing.T) {
			s, base, j := newQQOfficialTest(t)
			sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
			if _, err := s.handler.chatManager.CreateSession(sid, "test", "", ""); err != nil {
				t.Fatal(err)
			}
			expected := []storage.ChatMessage{{Role: "user", Content: "hi"}}
			if err := s.handler.chatManager.AddMessages(sid, expected); err != nil {
				t.Fatal(err)
			}
			record, _ := s.handler.chatManager.GetSession(sid)
			expected = record.Messages
			want := expected
			conn := &deadlineQQOfficial{fakeQQOfficial: base}
			conn.beforeDeadline = func() {
				switch change {
				case "disabled", "revoked":
					cfg := j.cfg
					if change == "disabled" {
						cfg.Enabled = false
					} else {
						cfg.SourceFilterMode = "allowlist"
						cfg.AllowedUserOpenIDs = []string{"someone-else"}
					}
					if err := s.handler.configManager.UpdateQQOfficialConfig(cfg); err != nil {
						t.Fatal(err)
					}
				case "generation":
					s.mu.Lock()
					s.generation++
					s.mu.Unlock()
				case "closed":
					s.Close()
				case "edited":
					if err := s.handler.chatManager.AddMessages(sid, []storage.ChatMessage{{Role: "user", Content: "edited"}}); err != nil {
						t.Fatal(err)
					}
					record, _ := s.handler.chatManager.GetSession(sid)
					want = record.Messages
				case "deleted":
					if err := s.handler.chatManager.DeleteSession(sid); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Keep shutdown cancellation independent so this specifically tests deadline
			// expiry even when configuration or history changes before the commit.
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			j.ctx, j.conn = ctx, conn
			reply := &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "first→second→third"}}}
			err := s.deliver(j, sid, "deadline-event", expected, reply)
			if change == "edited" || change == "deleted" {
				if err == nil {
					t.Fatal("stale history commit accepted")
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v", err)
			}
			if conn.calls != 2 || !reflect.DeepEqual(base.sent, []string{"first"}) {
				t.Fatalf("calls=%d confirmed=%v", conn.calls, base.sent)
			}
			record, exists := s.handler.chatManager.GetSession(sid)
			if change == "deleted" {
				if exists {
					t.Fatal("deleted session resurrected")
				}
				return
			}
			if !exists {
				t.Fatal("session lost")
			}
			if change != "none" {
				if !reflect.DeepEqual(record.Messages, want) {
					t.Fatalf("revoked/stale confirmation changed history: %#v", record.Messages)
				}
				return
			}
			if len(record.Messages) != 3 || record.Messages[1].Role != "assistant" || record.Messages[1].Content != "first" || record.Messages[2].Role != "system" {
				t.Fatalf("confirmed prefix lost: %#v", record.Messages)
			}
			if !reflect.DeepEqual(record.AppliedOperations, []string{"deadline-event-out"}) {
				t.Fatalf("missing commit receipt: %v", record.AppliedOperations)
			}
		})
	}
}
