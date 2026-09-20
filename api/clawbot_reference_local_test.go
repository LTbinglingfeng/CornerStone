package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"strings"
	"testing"
	"time"
)

func TestClawBotSentMediaReferenceUsesLocalHistoryCopy(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	j := stageMediaReply(t, s, cfg, "", []storage.ChatMessage{{Role: "assistant", Attachments: []storage.ChatAttachment{a}}})
	j = mediaDrive(t, s, cfg, j)
	if j.Status != "done" || len(f.items) != 1 {
		t.Fatalf("send=%+v", j)
	}
	if err := s.handler.attachmentStore().Remove(a.Path); err != nil {
		t.Fatal(err)
	}
	msg := newClawBotTextIncomingMessage("user", "explain this")
	msg.MessageID = "200"
	msg.ItemList[0].RefMsg = &client.ClawBotReference{ServerID: client.ClawBotMessageID(j.Chunks[0].MessageID)}
	restored, local := s.prepareClawBotReferences(cfg, msg)
	if len(local) != 1 || local[0].Path != j.Chunks[0].Media.HistoryPath || len(restored.ItemList) != 2 {
		t.Fatalf("local quote=%+v items=%+v", local, restored.ItemList)
	}
	// The fake server has no download endpoint: a successful preparation must use
	// local bytes, not the expired CDN reference or the deleted tool source.
	inbound := newDeliveryJob("chat", "user", time.Now())
	inbound.Inputs = []client.ClawBotIncomingMessage{msg}
	deliveryTestInsert(t, s, cfg, inbound)
	if _, err := s.prepareDeliveryInput(context.Background(), cfg, &inbound); err != nil {
		t.Fatal(err)
	}
	if len(inbound.InputMessages) != 1 || len(inbound.InputMessages[0].Attachments) != 1 || inbound.InputMessages[0].Attachments[0].Path != local[0].Path {
		t.Fatalf("restored input=%+v", inbound.InputMessages)
	}
}

func TestClawBotReferenceCachedAttachmentBudget(t *testing.T) {
	s, cfg, _ := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	original := client.ClawBotIncomingMessage{MessageID: "100", MessageType: 1, FromUserID: "user"}
	var metadata []storage.ClawBotReferenceMedia
	for i := 0; i < 17; i++ {
		original.ItemList = append(original.ItemList, client.ClawBotIncomingMessageItem{Type: 4, FileItem: &client.ClawBotFileItem{FileName: a.Name, Media: &client.ClawBotCDNMedia{EncryptQueryParam: "expired", AESKey: "bad"}}})
		metadata = append(metadata, storage.ClawBotReferenceMedia{ItemIndex: i, Attachment: a})
	}
	s.cacheClawBotReference(cfg, original)
	s.saveClawBotLocalReference(cfg, original, metadata)
	msg := newClawBotTextIncomingMessage("user", "quote")
	msg.MessageID = "200"
	msg.ItemList[0].RefMsg = &client.ClawBotReference{ServerID: "100"}
	j := newDeliveryJob("chat", "user", time.Now())
	j.Inputs = []client.ClawBotIncomingMessage{msg}
	deliveryTestInsert(t, s, cfg, j)
	if _, err := s.prepareDeliveryInput(context.Background(), cfg, &j); err != nil {
		t.Fatal(err)
	}
	if len(j.InputMessages) != 1 || len(j.InputMessages[0].Attachments) != 16 || !strings.Contains(j.InputMessages[0].Content, "超限") {
		t.Fatalf("budget bypass: %+v", j.InputMessages)
	}
}

func TestClawBotItemPreparationPreservesVoiceTranscript(t *testing.T) {
	s, cfg, _ := mediaDeliveryFixture(t)
	msg := newClawBotTextIncomingMessage("user", "caption")
	msg.ItemList = append(msg.ItemList, client.ClawBotIncomingMessageItem{Type: 3, VoiceItem: &client.ClawBotVoiceItem{Text: "voice transcript"}})
	j := newDeliveryJob("chat", "user", time.Now())
	j.Inputs = []client.ClawBotIncomingMessage{msg}
	deliveryTestInsert(t, s, cfg, j)
	if _, err := s.prepareDeliveryInput(context.Background(), cfg, &j); err != nil {
		t.Fatal(err)
	}
	if len(j.InputMessages) != 1 || !strings.Contains(j.InputMessages[0].Content, "caption") || !strings.Contains(j.InputMessages[0].Content, "voice transcript") {
		t.Fatalf("lost transcript: %+v", j.InputMessages)
	}
}
