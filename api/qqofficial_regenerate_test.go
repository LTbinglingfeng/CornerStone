package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestQQOfficialRegenerateReplacesWholeResponseAndDeduplicates(t *testing.T) {
	s, transport, j := newQQOfficialTest(t)
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	old, _ := s.handler.chatManager.GetSession(sid)
	calls := 0
	s.generate = func(_ context.Context, options sessionReplyOptions) (*generatedSessionReply, error) {
		calls++
		if options.Session.SessionID != sid || len(options.Session.Messages) != 1 || options.Session.Messages[0].Content != "hi" {
			t.Fatalf("generation did not trim previous response: %+v", options.Session)
		}
		if options.Channel != chatToolChannelQQOfficial || options.ToolOptions.Channel != chatToolChannelQQOfficial || len(options.ExtraSystemGuides) == 0 {
			t.Fatal("regeneration lost channel restrictions")
		}
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "replacement"}}}, nil
	}
	j.msg.ID, j.msg.Content = "regenerate-event", "/re"
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	record, _ := s.handler.chatManager.GetSession(sid)
	if len(record.Messages) != 2 || record.Messages[1].Content != "replacement" || !reflect.DeepEqual(record.Messages[0], old.Messages[0]) {
		t.Fatalf("incorrect replacement: %+v", record.Messages)
	}
	s.receipts = nil
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(transport.sent, []string{"hello", "replacement"}) {
		t.Fatalf("duplicate regeneration: calls=%d sent=%v", calls, transport.sent)
	}
}

func TestQQOfficialRegenerateFailuresKeepOriginalHistory(t *testing.T) {
	for _, mode := range []string{"generation", "first-send", "empty", "no-reply"} {
		t.Run(mode, func(t *testing.T) {
			s, transport, j := newQQOfficialTest(t)
			if err := s.process(j); err != nil {
				t.Fatal(err)
			}
			sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
			original, _ := s.handler.chatManager.GetSession(sid)
			s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
				switch mode {
				case "generation":
					return nil, errors.New("generation failed")
				case "empty":
					return &generatedSessionReply{}, nil
				case "no-reply":
					return &generatedSessionReply{NoReply: true, StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "not sent"}}}, nil
				}
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "replacement"}}}, nil
			}
			if mode == "first-send" {
				transport.failAt = 1
			}
			j.msg.ID, j.msg.Content = "regenerate", "/re"
			err := s.process(j)
			if (err != nil) != (mode == "generation" || mode == "first-send") {
				t.Fatalf("unexpected result: %v", err)
			}
			record, _ := s.handler.chatManager.GetSession(sid)
			if !reflect.DeepEqual(record.Messages, original.Messages) {
				t.Fatalf("failed regeneration replaced original: %+v", record.Messages)
			}
			sent := len(transport.sent)
			if err := s.process(j); err != nil || len(transport.sent) != sent {
				t.Fatalf("failed regeneration replayed: %v", err)
			}
		})
	}
}

func TestQQOfficialRegeneratePartialDeliveryOnlySavesConfirmedPrefix(t *testing.T) {
	s, transport, j := newQQOfficialTest(t)
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "first→second→third"}}}, nil
	}
	transport.failAt = 2
	j.msg.ID, j.msg.Content = "regenerate", "/re"
	if err := s.process(j); err == nil {
		t.Fatal("expected partial send failure")
	}
	record, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(record.Messages) != 3 || record.Messages[1].Content != "first" || record.Messages[2].Role != "system" {
		t.Fatalf("incorrect partial history: %+v", record.Messages)
	}
	if !reflect.DeepEqual(transport.sent, []string{"hello", "first"}) {
		t.Fatalf("unexpected delivery: %v", transport.sent)
	}
}

func TestQQOfficialRegenerateHistoryChangesRevokeDelivery(t *testing.T) {
	for _, change := range []string{"edit", "delete", "configuration"} {
		t.Run(change, func(t *testing.T) {
			s, transport, j := newQQOfficialTest(t)
			if err := s.process(j); err != nil {
				t.Fatal(err)
			}
			sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
			original, _ := s.handler.chatManager.GetSession(sid)
			s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
				switch change {
				case "edit":
					if err := s.handler.chatManager.AddMessages(sid, []storage.ChatMessage{{Role: "user", Content: "new history"}}); err != nil {
						t.Fatal(err)
					}
				case "delete":
					if err := s.handler.chatManager.DeleteSession(sid); err != nil {
						t.Fatal(err)
					}
				case "configuration":
					cfg := s.handler.configManager.GetQQOfficialConfig()
					cfg.AppID = "456"
					if err := s.handler.configManager.UpdateQQOfficialConfig(cfg); err != nil {
						t.Fatal(err)
					}
				}
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "stale"}}}, nil
			}
			j.msg.ID, j.msg.Content = "regenerate", "/re"
			if err := s.process(j); err == nil {
				t.Fatal("expected revoked delivery")
			}
			if !reflect.DeepEqual(transport.sent, []string{"hello"}) {
				t.Fatalf("stale reply sent: %v", transport.sent)
			}
			record, exists := s.handler.chatManager.GetSession(sid)
			if change == "delete" {
				if exists {
					t.Fatal("deleted session resurrected")
				}
			} else {
				want := original.Messages
				if change == "edit" {
					if len(record.Messages) != 3 || record.Messages[2].Content != "new history" {
						t.Fatal("edited history overwritten")
					}
					want = record.Messages[:2]
				}
				if !reflect.DeepEqual(want, original.Messages) || record.Messages[1].Content != "hello" {
					t.Fatal("original history overwritten")
				}
			}
		})
	}
}

func TestQQOfficialRegenerateToolReplayAndMediaAuthorization(t *testing.T) {
	for _, mediaSource := range []string{"input", "old-response"} {
		t.Run(mediaSource, func(t *testing.T) {
			s, transport, j := newQQOfficialTest(t)
			path, err := s.handler.saveCachePhotoBytes([]byte("\x89PNG\r\n\x1a\n"))
			if err != nil {
				t.Fatal(err)
			}
			if err := s.process(j); err != nil {
				t.Fatal(err)
			}
			sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
			record, _ := s.handler.chatManager.GetSession(sid)
			messages := append([]storage.ChatMessage(nil), record.Messages...)
			index := 0
			if mediaSource == "old-response" {
				index = 1
			}
			messages[index].ImagePaths = []string{path}
			if err := s.handler.chatManager.CommitDeliverySuffix(sid, "seed-image", record.Messages, messages); err != nil {
				t.Fatal(err)
			}
			state := &client.ProviderState{Provider: "anthropic", Model: "test", Payload: json.RawMessage(`{"thinking":"signed"}`)}
			s.generate = func(_ context.Context, options sessionReplyOptions) (*generatedSessionReply, error) {
				if len(options.Session.Messages) != 1 {
					t.Fatal("old response leaked to model")
				}
				result, _ := json.Marshal(chatToolResult{OK: true, Tool: sendAttachmentToolName, Data: map[string]any{"attachment": storage.ChatAttachment{Path: path, Kind: "image"}}})
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{
					{Role: "assistant", ToolCalls: []client.ToolCall{{ID: "image-call", Type: "function", Function: client.ToolCallFunction{Name: sendAttachmentToolName}}}, ProviderState: state},
					{Role: "tool", ToolCallID: "image-call", Content: string(result)},
					{Role: "assistant", Content: "replacement", ProviderState: state},
				}}, nil
			}
			j.msg.ID, j.msg.Content = "regenerate", "/re"
			err = s.process(j)
			if mediaSource == "old-response" {
				if err == nil || len(transport.sent) != 1 {
					t.Fatalf("trimmed media authorized: err=%v sent=%v", err, transport.sent)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			record, _ = s.handler.chatManager.GetSession(sid)
			if len(record.Messages) != 5 || record.Messages[1].ProviderState == nil || record.Messages[2].Role != "tool" || !strings.Contains(record.Messages[2].Content, "delivery_confirmed") || record.Messages[3].Content != "replacement" {
				t.Fatalf("invalid provider replay: %+v", record.Messages)
			}
			if len(transport.sent) != 3 || !strings.HasPrefix(transport.sent[1], "image:") || transport.sent[2] != "replacement" {
				t.Fatalf("incorrect media delivery: %v", transport.sent)
			}
		})
	}
}
