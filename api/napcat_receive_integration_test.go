package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNapCatReceivePrivateMediaQuoteAndForwardPersist(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		switch req.Action {
		case "get_login_info":
			return napCatActionResponse{Data: json.RawMessage(`{"user_id":20002}`)}
		case "get_msg":
			return napCatActionResponse{Data: json.RawMessage(`{"message_id":88,"message_type":"private","sender":{"user_id":10001,"nickname":"Alice"},"message":[{"type":"file","data":{"file_id":"quoted","file":"quoted.txt"}}]}`)}
		case "get_private_file_url":
			return napCatActionResponse{Status: "failed", RetCode: 1}
		case "get_file":
			return napCatActionResponse{Data: json.RawMessage(`{"base64":"aGVsbG8="}`)}
		case "get_record":
			return napCatActionResponse{Data: json.RawMessage(`{"base64":"SUQz"}`)}
		case "get_forward_msg":
			return napCatActionResponse{Data: json.RawMessage(`{"messages":[{"sender":{"nickname":"Bob"},"content":[{"type":"text","data":{"text":"forwarded text"}},{"type":"file","data":{"file_id":"file","file":"report.txt"}}]}]}`)}
		case "send_private_msg":
			return napCatActionResponse{}
		default:
			t.Errorf("unexpected action: %s", req.Action)
			return napCatActionResponse{}
		}
	})
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	pending, ok := s.parseIncomingPrivateMessage(source, napCatMessageEvent{MessageID: 123, Message: json.RawMessage(`[{"type":"reply","data":{"id":"88"}},{"type":"record","data":{"file":"voice"}},{"type":"video","data":{"file":"video"}},{"type":"forward","data":{"id":"forward"}}]`)})
	if !ok {
		t.Fatal("media-only input discarded")
	}
	s.processIncomingBatch(context.Background(), h.configManager.GetNapCatConfig(), source, []napCatPendingMessage{*pending})
	sessions := h.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatal(sessions)
	}
	record, _ := h.chatManager.GetSession(sessions[0].ID)
	msg := record.Messages[0]
	if msg.Role != "user" || len(msg.Attachments) != 3 || msg.Quote == nil || len(msg.Quote.Attachments) != 1 {
		t.Fatalf("history: %+v", msg)
	}
	if msg.Attachments[2].Name != "report.txt" || msg.Quote.Attachments[0].Name != "quoted.txt" || !strings.Contains(msg.Content, "forwarded text") {
		t.Fatalf("lost media or forward metadata: %+v", msg)
	}
	if !strings.Contains(msg.Content, "未转写或读取") || !strings.Contains(msg.Quote.Content, "未转写或读取") {
		t.Fatal("missing model capability boundary")
	}
	for _, a := range append(msg.Attachments, msg.Quote.Attachments...) {
		if _, err := h.attachmentStore().Read(a.Path); err != nil {
			t.Fatal(err)
		}
	}
	msg.Quote.Attachments[0].Name = "mutated"
	again, _ := h.chatManager.GetSession(sessions[0].ID)
	if again.Messages[0].Quote.Attachments[0].Name != "quoted.txt" {
		t.Fatal("quote metadata is not cloned")
	}
}

func TestNapCatReceiveUnsupportedGroupStillIgnored(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	s := NewNapCatService(h)
	defer s.Close()
	s.handleEvent(context.Background(), napCatMessageEvent{PostType: "message", MessageType: "group", SelfID: 20002, UserID: 10001, GroupID: 42, Message: json.RawMessage(`[{"type":"file","data":{"file_id":"file"}}]`)})
	if len(s.pendingReplies) != 0 || len(h.chatManager.ListSessions()) != 0 {
		t.Fatal("group message entered private pipeline")
	}
}
