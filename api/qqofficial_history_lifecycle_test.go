package api

import (
	"context"
	"cornerstone/storage"
	"testing"
)

func TestQQOfficialHistoryChangesRevokePendingReply(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "edited"
		if remove {
			name = "deleted"
		}
		t.Run(name, func(t *testing.T) {
			s, transport, job := newQQOfficialTest(t)
			sid := qqOfficialSessionID(job.cfg, job.msg.UserOpenID)
			s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
				if remove {
					if err := s.handler.chatManager.DeleteSession(sid); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := s.handler.chatManager.AddMessages(sid, []storage.ChatMessage{{Role: "user", Content: "new history"}}); err != nil {
						t.Fatal(err)
					}
				}
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "old reply must not send"}}}, nil
			}
			if err := s.process(job); err == nil {
				t.Fatal("stale delivery accepted")
			}
			if len(transport.sent) != 0 {
				t.Fatalf("sent after history changed: %v", transport.sent)
			}
			record, exists := s.handler.chatManager.GetSession(sid)
			if remove && exists {
				t.Fatal("deleted session resurrected")
			}
			if !remove && (!exists || len(record.Messages) != 2 || record.Messages[1].Content != "new history") {
				t.Fatal("edited history overwritten")
			}
		})
	}
}

func TestQQOfficialDeletedSessionReplayUsesIndependentReceipt(t *testing.T) {
	s, transport, job := newQQOfficialTest(t)
	if err := s.process(job); err != nil {
		t.Fatal(err)
	}
	sid := qqOfficialSessionID(job.cfg, job.msg.UserOpenID)
	if err := s.handler.chatManager.DeleteSession(sid); err != nil {
		t.Fatal(err)
	}
	// Reopen the store from disk, not merely an in-memory deduplication cache.
	s.receipts = nil
	if err := s.process(job); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("replayed external message: %v", transport.sent)
	}
	if _, exists := s.handler.chatManager.GetSession(sid); exists {
		t.Fatal("replayed event recreated deleted history")
	}
}
