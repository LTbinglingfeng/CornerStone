package api

import (
	"cornerstone/client"
	"cornerstone/storage"
	"testing"
	"time"
)

func TestClawBotDeliveryUploadedReceiptSurvivesRestart(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	j := stageMediaReply(t, s, cfg, "", []storage.ChatMessage{{Role: "assistant", Attachments: []storage.ChatAttachment{a}}})
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Chunks[0].Media.Uploaded == nil || len(f.items) != 0 {
		t.Fatal("not staged")
	}
	if err := s.handler.attachmentStore().Remove(a.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverDeliveries(cfg); err != nil {
		t.Fatal(err)
	}
	j = mediaDrive(t, s, cfg, deliveryTestJob(t, s, j.ID))
	if j.Status != "done" || f.uploads != 1 || len(f.items) != 1 {
		t.Fatalf("receipt reuploaded/resend: uploads=%d sends=%d state=%s", f.uploads, len(f.items), j.Status)
	}
	// Confirmed history can be restored without any network activity.
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverDeliveries(cfg); err != nil {
		t.Fatal(err)
	}
	if j = deliveryTestJob(t, s, j.ID); j.Status != "done" || !j.HistoryCommitted {
		t.Fatal("history receipt lost")
	}
}

// An interrupted send counts as delivered on restart: never replayed, and no
// manual confirmation is needed.
func TestClawBotDeliveryInterruptedMediaSendNeverAutoReplays(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	j := stageMediaReply(t, s, cfg, "", []storage.ChatMessage{{Role: "assistant", Attachments: []storage.ChatAttachment{a}}})
	j = deliveryTestStep(t, s, cfg, j.ID)
	if err := s.changeDelivery(cfg, j.ID, func(j *storage.ClawBotDeliveryJob) error {
		j.Status = "sending"
		j.Chunks[0].Status = "sending"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverDeliveries(cfg); err != nil {
		t.Fatal(err)
	}
	j = mediaDrive(t, s, cfg, deliveryTestJob(t, s, j.ID))
	if j.Status != "done" || !j.HistoryCommitted || j.Chunks[0].Status != "sent" || len(f.items) != 0 || f.uploads != 1 {
		t.Fatalf("replayed interrupted send: %+v", j)
	}
}

func TestClawBotDeliveryMediaNoReplyAndToolCallDedup(t *testing.T) {
	a := storage.ChatAttachment{Path: "cache_photo/artifact.png", Kind: "image", Name: "artifact.png"}
	result := marshalChatToolResult(chatToolResult{OK: true, Tool: "send_attachment", Data: map[string]any{"attachment": a}})
	messages := []storage.ChatMessage{{Role: "tool", ToolCallID: "same-call", Content: result}, {Role: "tool", ToolCallID: "same-call", Content: result}, {Role: "assistant", Content: "after"}}
	chunks := buildClawBotDeliveryChunks("job", "after", "→", messages)
	if len(chunks) != 2 || chunks[0].Media == nil || chunks[1].Text != "after" {
		t.Fatalf("duplicate artifact: %+v", chunks)
	}
	messages = append(messages, storage.ChatMessage{Role: "assistant", ToolCalls: []client.ToolCall{{ID: "silent", Type: "function", Function: client.ToolCallFunction{Name: "no_reply"}}}, Timestamp: time.Now()})
	messages = append(messages, storage.ChatMessage{Role: "tool", ToolCallID: "silent", Content: marshalChatToolResult(chatToolResult{Tool: "no_reply", OK: false, Error: "disabled"})})
	if got := buildClawBotDeliveryChunks("job", "after", "→", messages); len(got) != 2 {
		t.Fatal("failed no_reply suppressed real reply")
	}
	messages[len(messages)-1].Content = marshalChatToolResult(chatToolResult{Tool: "no_reply", OK: true})
	if got := buildClawBotDeliveryChunks("job", "after", "→", messages); len(got) != 0 {
		t.Fatal("no_reply leaked attachment")
	}
}
