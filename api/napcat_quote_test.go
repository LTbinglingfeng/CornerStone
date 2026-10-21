package api

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNapCatQuoteProvenance(t *testing.T) {
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	base := napCatGetMsgData{MessageID: 88, MessageType: "private", UserID: 10001, Message: json.RawMessage(`[{"type":"text","data":{"text":"allowed"}}]`)}
	base.Sender.UserID = 10001
	for _, tc := range []struct {
		name    string
		change  func(*napCatGetMsgData, *napCatChatSource)
		allowed bool
	}{
		{"peer message", func(*napCatGetMsgData, *napCatChatSource) {}, true},
		{"sender fallback", func(d *napCatGetMsgData, _ *napCatChatSource) { d.Sender.UserID = 0 }, true},
		{"sender only", func(d *napCatGetMsgData, _ *napCatChatSource) { d.UserID = 0 }, true},
		{"explicit recipient", func(d *napCatGetMsgData, _ *napCatChatSource) { d.TargetID = 20002 }, true},
		{"bot with peer", func(d *napCatGetMsgData, _ *napCatChatSource) {
			d.Sender.UserID = 20002
			d.UserID = 20002
			d.TargetID = 10001
		}, true},
		{"other sender", func(d *napCatGetMsgData, _ *napCatChatSource) { d.Sender.UserID = 30003; d.UserID = 30003 }, false},
		{"bot without peer", func(d *napCatGetMsgData, _ *napCatChatSource) { d.Sender.UserID = 20002; d.UserID = 20002 }, false},
		{"bot other peer", func(d *napCatGetMsgData, _ *napCatChatSource) {
			d.Sender.UserID = 20002
			d.UserID = 20002
			d.TargetID = 30003
		}, false},
		{"wrong target", func(d *napCatGetMsgData, _ *napCatChatSource) { d.TargetID = 30003 }, false},
		{"conflicting sender", func(d *napCatGetMsgData, _ *napCatChatSource) { d.UserID = 30003 }, false},
		{"missing sender", func(d *napCatGetMsgData, _ *napCatChatSource) { d.UserID = 0; d.Sender.UserID = 0 }, false},
		{"group", func(d *napCatGetMsgData, _ *napCatChatSource) { d.MessageType = "group" }, false},
		{"hidden group", func(d *napCatGetMsgData, _ *napCatChatSource) { d.GroupID = 42 }, false},
		{"missing type", func(d *napCatGetMsgData, _ *napCatChatSource) { d.MessageType = "" }, false},
		{"wrong message", func(d *napCatGetMsgData, _ *napCatChatSource) { d.MessageID = 89 }, false},
		{"missing message", func(d *napCatGetMsgData, _ *napCatChatSource) { d.MessageID = 0 }, false},
		{"other bot", func(d *napCatGetMsgData, _ *napCatChatSource) { d.SelfID = 30003 }, false},
		{"missing source bot", func(_ *napCatGetMsgData, s *napCatChatSource) { s.SelfID = 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, src := base, source
			tc.change(&d, &src)
			if err := validateNapCatQuoteSource(src, 88, d); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v", tc.allowed, err)
			}
			if !tc.allowed {
				// A nil handler would panic on materialization. Rejection must occur
				// before parsing/resource fetching, not after saving foreign media.
				d.Message = json.RawMessage(`[{"type":"file","data":{"file_id":"foreign"}}]`)
				q, err := (&NapCatService{}).buildQuotedMessageFromGetMsg(context.Background(), src, 88, d)
				if err == nil || q != nil {
					t.Fatalf("foreign quote accepted: %+v, %v", q, err)
				}
			}
		})
	}
}

func TestNapCatRejectedQuotePreservesCurrentInput(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		switch req.Action {
		case "get_login_info":
			return napCatActionResponse{Data: json.RawMessage(`{"user_id":20002}`)}
		case "get_msg":
			return napCatActionResponse{Data: json.RawMessage(`{"message_id":88,"message_type":"private","sender":{"user_id":30003},"message":[{"type":"file","data":{"file_id":"foreign"}}]}`)}
		case "send_private_msg":
			return napCatActionResponse{}
		default:
			t.Errorf("foreign media must not be fetched: %s", req.Action)
			return napCatActionResponse{}
		}
	})
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	pending, ok := s.parseIncomingPrivateMessage(source, napCatMessageEvent{MessageID: 123, Message: json.RawMessage(`[{"type":"reply","data":{"id":"88"}},{"type":"text","data":{"text":"keep me"}}]`)})
	if !ok {
		t.Fatal("input discarded")
	}
	s.processIncomingBatch(context.Background(), h.configManager.GetNapCatConfig(), source, []napCatPendingMessage{*pending})
	sessions := h.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatal(sessions)
	}
	record, _ := h.chatManager.GetSession(sessions[0].ID)
	msg := record.Messages[0]
	if msg.Content != "keep me" || msg.Quote == nil || msg.Quote.Content != "[引用消息内容不可用]" || len(msg.Quote.Attachments) != 0 || len(msg.Quote.ImagePaths) != 0 {
		t.Fatalf("unexpected persisted message: %+v", msg)
	}
}
