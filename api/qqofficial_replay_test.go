package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"testing"
)

func TestQQOfficialImageToolBatchKeepsProviderReplayValid(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "complete"
		if fail {
			name = "final-text-failed"
		}
		t.Run(name, func(t *testing.T) {
			s, transport, job := newQQOfficialTest(t)
			if fail {
				transport.failAt = 2
			}
			s.download = func(context.Context, string) ([]byte, error) { return []byte("\x89PNG\r\n\x1a\n"), nil }
			job.msg.Attachments = []client.QQOfficialAttachment{{ContentType: "image/png"}}
			state := &client.ProviderState{Provider: "anthropic", Model: "test", Payload: json.RawMessage(`{"thinking":"opaque signed state"}`)}
			s.generate = func(_ context.Context, options sessionReplyOptions) (*generatedSessionReply, error) {
				path := options.Session.Messages[0].ImagePaths[0]
				result, _ := json.Marshal(chatToolResult{OK: true, Tool: sendAttachmentToolName, Data: map[string]any{"attachment": storage.ChatAttachment{Path: path, Kind: "image"}}})
				calls := []client.ToolCall{{ID: "one", Type: "function", Function: client.ToolCallFunction{Name: sendAttachmentToolName}}, {ID: "two", Type: "function", Function: client.ToolCallFunction{Name: sendAttachmentToolName}}}
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{
					{Role: "assistant", Content: "internal tool commentary", ReasoningContent: "reasoning", ToolCalls: calls, ProviderState: state},
					{Role: "tool", ToolCallID: "one", Content: string(result)},
					{Role: "tool", ToolCallID: "two", Content: string(result)},
					{Role: "assistant", Content: "final", ProviderState: state},
				}}, nil
			}
			err := s.process(job)
			if (err != nil) != fail {
				t.Fatalf("unexpected send result: %v", err)
			}
			record, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(job.cfg, job.msg.UserOpenID))
			messages := record.Messages
			if len(messages) < 5 || len(messages[1].ToolCalls) != 2 || messages[2].Role != "tool" || messages[3].Role != "tool" {
				t.Fatalf("tool batch interrupted: %+v", messages)
			}
			if messages[1].ProviderState == nil || messages[1].Content != "" {
				t.Fatal("lost signed tool state or persisted unsent commentary")
			}
			for _, m := range messages[4:] {
				if fail && m.ProviderState != nil {
					t.Fatal("undelivered final continuation state retained")
				}
			}
			if len(transport.sent) < 1 || transport.sent[0] == "internal tool commentary" {
				t.Fatal("unexpected visible tool commentary")
			}
			if !fail && (len(transport.sent) != 2 || transport.sent[1] != "final") {
				t.Fatalf("wrong delivery order/dedupe: %v", transport.sent)
			}
		})
	}
}
