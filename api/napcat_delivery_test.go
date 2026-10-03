package api

import (
	"context"
	"cornerstone/storage"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func napCatDeliveryTestService(t *testing.T, respond func(napCatActionRequest) *napCatActionResponse) *NapCatService {
	t.Helper()
	dir := t.TempDir()
	h := &Handler{cachePhotoDir: filepath.Join(dir, "cache_photo"), ttsAudioDir: filepath.Join(dir, "tts_audio"), chatManager: storage.NewChatManager(filepath.Join(dir, "chats"))}
	for _, d := range []string{h.cachePhotoDir, h.ttsAudioDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s := &NapCatService{handler: h, pendingActionWaiters: map[string]chan *napCatActionResponse{}}
	up := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var req napCatActionRequest
			if conn.ReadJSON(&req) != nil {
				return
			}
			resp := respond(req)
			if resp == nil {
				continue
			}
			s.mu.Lock()
			ch := s.pendingActionWaiters[req.Echo]
			delete(s.pendingActionWaiters, req.Echo)
			if ch != nil {
				ch <- resp
			}
			s.mu.Unlock()
		}
	}))
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.conn = conn
	t.Cleanup(func() { conn.Close(); server.Close() })
	return s
}

func TestNapCatPrivateAttachmentWireAndConfirmation(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	gate := make(chan struct{})
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse {
		requests <- r
		<-gate
		return &napCatActionResponse{Status: "ok"}
	})
	for _, kind := range []string{"image", "audio", "video", "file"} {
		var a storage.ChatAttachment
		if kind == "image" {
			a = storage.ChatAttachment{Path: "cache_photo/photo.png", Kind: kind}
			if err := os.WriteFile(filepath.Join(s.handler.cachePhotoDir, "photo.png"), []byte("payload"), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			var err error
			a, err = s.handler.attachmentStore().Save([]byte("payload"), "original.bin", kind)
			if err != nil {
				t.Fatal(err)
			}
		}
		done := make(chan error, 1)
		go func() { done <- s.sendPrivateAttachment(context.Background(), 42, a) }()
		var req napCatActionRequest
		select {
		case req = <-requests:
		case <-time.After(time.Second):
			t.Fatal("no action")
		}
		select {
		case <-done:
			t.Fatal("returned before confirmation")
		default:
		}
		b, _ := json.Marshal(req.Params)
		if !strings.Contains(string(b), "base64://"+base64.StdEncoding.EncodeToString([]byte("payload"))) || strings.Contains(string(b), s.handler.cachePhotoDir) {
			t.Fatalf("wire: %s", b)
		}
		if kind == "file" {
			if req.Action != "upload_private_file" || !strings.Contains(string(b), `"name":"original.bin"`) {
				t.Fatalf("file: %s", b)
			}
		} else {
			want := kind
			if want == "audio" {
				want = "record"
			}
			if req.Action != "send_private_msg" || !strings.Contains(string(b), `"type":"`+want+`"`) {
				t.Fatalf("media: %s", b)
			}
		}
		gate <- struct{}{}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestNapCatOutgoingPathsConfined(t *testing.T) {
	s := napCatDeliveryTestService(t, func(napCatActionRequest) *napCatActionResponse {
		t.Error("unexpected send")
		return &napCatActionResponse{Status: "ok"}
	})
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.handler.cachePhotoDir, "escape.png")); err != nil {
		t.Fatal(err)
	}
	a, err := s.handler.attachmentStore().Save([]byte("ok"), "a.bin", "file")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(s.handler.attachmentStore().Dir, strings.TrimPrefix(a.Path, storage.AttachmentURLPrefix))
	os.Remove(target)
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, "https://example.com/a", "file:///tmp/a", "cache_photo/../secret", "cache_photo/escape.png", "tts_audio/a/b", "cache_photo/%2e%2e", a.Path} {
		if _, err := s.handler.readNapCatOutgoingAttachment(storage.ChatAttachment{Path: p}); err == nil {
			t.Errorf("accepted %s", p)
		}
	}
}

func TestNapCatGeneratedReplyPartialDelivery(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	count := 0
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse {
		requests <- r
		count++
		if count == 2 {
			return &napCatActionResponse{Status: "failed", RetCode: 1}
		}
		return &napCatActionResponse{Status: "ok"}
	})
	if _, err := s.handler.chatManager.CreateSession("delivery", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	a, err := s.handler.attachmentStore().Save([]byte("a"), "a.bin", "file")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.handler.attachmentStore().Save([]byte("b"), "b.bin", "file")
	if err != nil {
		t.Fatal(err)
	}
	intent := func(id string, a storage.ChatAttachment) storage.ChatMessage {
		return storage.ChatMessage{Role: "tool", ToolCallID: id, Content: marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, OK: true, Data: map[string]interface{}{"attachment": a}})}
	}
	reply := &napCatGeneratedReply{Text: "Both files sent!", StorageMessages: []storage.ChatMessage{intent("a", a), intent("duplicate", a), intent("b", b), {Role: "assistant", Content: "Both files sent!", Attachments: []storage.ChatAttachment{a, b}}}}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "private", UserID: 42}, "delivery", reply); err == nil {
		t.Fatal("expected delivery failure")
	}
	if len(requests) != 2 {
		t.Fatalf("must deduplicate and stop, actions=%d", len(requests))
	}
	record, _ := s.handler.chatManager.GetSession("delivery")
	var confirmed, failed bool
	for _, m := range record.Messages {
		if strings.Contains(m.Content, "Both files sent!") {
			t.Fatal("persisted unsent text")
		}
		for _, att := range m.Attachments {
			if att.Path == b.Path {
				t.Fatal("persisted failed attachment")
			}
		}
		if m.ToolCallID == "a" {
			att, ok := attachmentFromToolResult(m.Content)
			confirmed = ok && att.Path == a.Path
		}
		if m.ToolCallID == "b" {
			_, ok := attachmentFromToolResult(m.Content)
			failed = !ok && strings.Contains(m.Content, "not confirmed")
		}
	}
	if !confirmed || !failed {
		t.Fatalf("history: %+v", record.Messages)
	}
	if len(reply.StorageMessages[3].Attachments) != 2 {
		t.Fatal("mutated generated input")
	}
}

func TestNapCatGeneratedReplyStructuredTextAndNoReply(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse {
		requests <- r
		return &napCatActionResponse{Status: "ok"}
	})
	if _, err := s.handler.chatManager.CreateSession("text", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	source := napCatChatSource{Kind: "private", UserID: 42}
	if err := s.sendGeneratedPrivateReply(context.Background(), source, "text", &napCatGeneratedReply{Text: "[CQ:image,file=secret]"}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal((<-requests).Params)
	if !strings.Contains(string(b), `"type":"text"`) || !strings.Contains(string(b), `"text":"[CQ:image,file=secret]"`) {
		t.Fatalf("not structured: %s", b)
	}
	reply := &napCatGeneratedReply{Text: "must not send", StorageMessages: []storage.ChatMessage{{Role: "tool", ToolCallID: "silent", Content: marshalChatToolResult(chatToolResult{Tool: "no_reply", OK: true})}, {Role: "assistant", ImagePaths: []string{"cache_photo/missing.png"}}}}
	if err := s.sendGeneratedPrivateReply(context.Background(), source, "text", reply); err != nil {
		t.Fatal(err)
	}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "group", UserID: 42}, "text", &napCatGeneratedReply{Text: "group"}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatal("sent silent/group reply")
	}
}

func TestNapCatAttachmentTimeoutDoesNotRetry(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse { requests <- r; return nil })
	a, err := s.handler.attachmentStore().Save([]byte("a"), "a.bin", "file")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := s.sendPrivateAttachment(ctx, 42, a); err == nil {
		t.Fatal("timeout treated as confirmation")
	}
	if len(requests) != 1 {
		t.Fatalf("retried: %d", len(requests))
	}
}

func TestNapCatAssistantMediaAndIntentSelection(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse {
		requests <- r
		return &napCatActionResponse{Status: "ok"}
	})
	if _, err := s.handler.chatManager.CreateSession("media", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(s.handler.cachePhotoDir, "a.png"), filepath.Join(s.handler.ttsAudioDir, "a.mp3")} {
		if err := os.WriteFile(p, []byte("media"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.handler.attachmentStore().Save([]byte("file"), "资料.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	reply := &napCatGeneratedReply{Text: "done", StorageMessages: []storage.ChatMessage{
		{Role: "tool", ToolCallID: "search", Content: marshalChatToolResult(chatToolResult{Tool: "search", OK: true, Data: map[string]interface{}{"attachment": storage.ChatAttachment{Path: "cache_photo/missing.png", Kind: "image"}}})},
		{Role: "tool", ToolCallID: "failed", Content: marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, OK: false, Data: map[string]interface{}{"attachment": storage.ChatAttachment{Path: "cache_photo/missing.png", Kind: "image"}}})},
		{Role: "assistant", Content: "done", ImagePaths: []string{"cache_photo/a.png"}, TTSAudioPaths: []string{"tts_audio/a.mp3"}, Attachments: []storage.ChatAttachment{a}},
	}}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "private", UserID: 42}, "media", reply); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 4 {
		t.Fatalf("actions=%d", len(requests))
	}
	record, _ := s.handler.chatManager.GetSession("media")
	count := 0
	for _, m := range record.Messages {
		if m.Role == "assistant" {
			count += len(m.ImagePaths) + len(m.TTSAudioPaths) + len(m.Attachments)
		}
	}
	if count != 3 {
		t.Fatalf("missing confirmed media: %+v", record.Messages)
	}
}

func TestNapCatSendAttachmentToolSessionWhitelist(t *testing.T) {
	executor := newChatToolExecutor()
	ctx := chatToolContext{Channel: chatToolChannelNapCat, Attachments: sessionToolAttachments([]storage.ChatMessage{{ImagePaths: []string{"cache_photo/registered.png"}}})}
	if !executor.ExecuteResult(context.Background(), toolCall("a", sendAttachmentToolName, `{"path":"cache_photo/registered.png"}`), ctx).OK {
		t.Fatal("registered media denied")
	}
	if executor.ExecuteResult(context.Background(), toolCall("b", sendAttachmentToolName, `{"path":"cache_photo/other.png"}`), ctx).OK {
		t.Fatal("whitelist bypass")
	}
	for _, name := range []string{"generate_image", "generate_tts", "generate_speech"} {
		if buildToolNameSet(getChatTools(chatToolOptions{Channel: chatToolChannelNapCat}))[name] {
			t.Fatalf("enabled %s", name)
		}
	}
}
