package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"testing"
)

func TestNapCatPrivateToolsAndAttachmentHistory(t *testing.T) {
	names := buildToolNameSet(getChatTools(chatToolOptions{Channel: chatToolChannelNapCat}))
	if !names["no_reply"] || !names[sendAttachmentToolName] {
		t.Fatalf("missing private reply controls: %+v", names)
	}
	if shouldDropEmptyAssistantStorageMessage(storage.ChatMessage{Role: "assistant", Attachments: []storage.ChatAttachment{{Path: "/api/attachments/example", Kind: "file"}}}) {
		t.Fatal("attachment-only assistant history was discarded")
	}
}

func TestNapCatReplyDeliveryEntryAndQuotedAttachmentCatalog(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	requests := make(chan napCatActionRequest, 4)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "get_login_info" {
			return napCatActionResponse{Data: json.RawMessage(`{"user_id":20002}`)}
		}
		requests <- req
		return napCatActionResponse{}
	})
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	session, err := s.createSessionForSource(source, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.attachmentStore().Save([]byte("quoted attachment"), "quote.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	catalog := sessionToolAttachments([]storage.ChatMessage{{Role: "user", Quote: &storage.QuotedMessage{Attachments: []storage.ChatAttachment{a}, ImagePaths: []string{"cache_photo/quoted.png"}}}})
	if catalog[a.Path].Name != a.Name || catalog["cache_photo/quoted.png"].Kind != "image" {
		t.Fatal("quoted media missing from session catalog")
	}
	reply := &napCatGeneratedReply{StorageMessages: []storage.ChatMessage{{Role: "tool", ToolCallID: "send", Content: marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, OK: true, Data: map[string]interface{}{"attachment": a}})}}}
	s.sendAndPersistReply(context.Background(), source, session.SessionID, reply)
	if len(requests) != 1 || (<-requests).Action != "upload_private_file" {
		t.Fatal("main reply entry did not deliver the attachment")
	}
	record, _ := h.chatManager.GetSession(session.SessionID)
	if len(record.Messages) != 2 || len(record.Messages[1].Attachments) != 1 {
		t.Fatalf("main entry history: %+v", record.Messages)
	}
	source.SelfID = 30003
	s.sendAndPersistReply(context.Background(), source, session.SessionID, reply)
	if len(requests) != 0 {
		t.Fatal("sent another account's reply")
	}
}

func TestNapCatToolOnlyAttachmentVisibleHistory(t *testing.T) {
	s := napCatDeliveryTestService(t, func(napCatActionRequest) *napCatActionResponse { return &napCatActionResponse{Status: "ok"} })
	if _, err := s.handler.chatManager.CreateSession("media-only", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	a, err := s.handler.attachmentStore().Save([]byte("document"), "document.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	reply := &napCatGeneratedReply{StorageMessages: []storage.ChatMessage{
		{Role: "assistant", ToolCalls: []client.ToolCall{toolCall("send", sendAttachmentToolName, `{"path":"`+a.Path+`"}`)}},
		{Role: "tool", ToolCallID: "send", Content: marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, OK: true, Data: map[string]interface{}{"attachment": a}})},
	}}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "private", UserID: 42}, "media-only", reply); err != nil {
		t.Fatal(err)
	}
	record, _ := s.handler.chatManager.GetSession("media-only")
	count := 0
	for _, m := range record.Messages {
		if m.Role == "assistant" {
			count += len(m.Attachments)
		}
	}
	if count != 1 {
		t.Fatalf("visible attachments = %d, history=%+v", count, record.Messages)
	}
}
