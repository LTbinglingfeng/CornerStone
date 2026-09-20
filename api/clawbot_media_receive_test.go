package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiveAttachmentFailureKeepsTranscriptAndHistory(t *testing.T) {
	s, cfg, _ := deliveryTestService(t, "https://example.test")
	s.handler.cachePhotoDir = filepath.Join(t.TempDir(), "cache_photo")
	msg := client.ClawBotIncomingMessage{FromUserID: "user", MessageType: 1, ItemList: []client.ClawBotIncomingMessageItem{
		{Type: 3, VoiceItem: &client.ClawBotVoiceItem{Text: "voice words", Media: &client.ClawBotCDNMedia{FullURL: "file:///secret"}}},
		{Type: 4, FileItem: &client.ClawBotFileItem{FileName: "notes.txt"}},
	}}
	job := storage.ClawBotDeliveryJob{ID: "media-input", UserID: "user", Status: "preparing", CreatedAt: time.Now(), Inputs: []client.ClawBotIncomingMessage{msg}}
	deliveryTestInsert(t, s, cfg, job)
	session, err := s.prepareDeliveryInput(context.Background(), cfg, &job)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Messages) != 1 || !strings.Contains(session.Messages[0].Content, "voice words") || !strings.Contains(session.Messages[0].Content, "内容不可用") {
		t.Fatalf("%+v", session.Messages)
	}
	again, err := s.prepareDeliveryInput(context.Background(), cfg, &job)
	if err != nil || len(again.Messages) != 1 {
		t.Fatal("retry duplicated history", err)
	}
}
func TestProtectedAttachmentDownload(t *testing.T) {
	s, _, _ := deliveryTestService(t, "https://example.test")
	s.handler.cachePhotoDir = filepath.Join(t.TempDir(), "cache_photo")
	a, err := s.handler.attachmentStore().Save([]byte("<script>evil</script>"), "active.html", "file")
	if err != nil {
		t.Fatal(err)
	}
	s.handler.authManager = storage.NewAuthManager(t.TempDir())
	s.handler.tokenStore = newAuthTokenStore()
	if _, err := s.handler.authManager.Setup("owner", "long-test-password"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.handler.registerProtectedRoute(mux, storage.AttachmentURLPrefix, s.handler.handleAttachment)
	denied := httptest.NewRecorder()
	mux.ServeHTTP(denied, httptest.NewRequest("GET", a.Path, nil))
	if denied.Code == http.StatusOK {
		t.Fatal("public attachment access")
	}
	token, err := s.handler.tokenStore.create("owner", "owner")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", a.Path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, request)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe attachment response: %+v", w.Result())
	}
}

func TestReceiveAttachmentTranscriptOnlyAndBatchLimit(t *testing.T) {
	s, _, _ := deliveryTestService(t, "https://example.test")
	s.handler.cachePhotoDir = filepath.Join(t.TempDir(), "cache_photo")
	attachments, notice := s.receiveClawBotAttachments(context.Background(), client.ClawBotIncomingMessage{ItemList: []client.ClawBotIncomingMessageItem{
		{Type: 1, TextItem: &client.ClawBotItemText{Text: "user words"}},
		{Type: 3, VoiceItem: &client.ClawBotVoiceItem{Text: "transcript words"}},
	}})
	if len(attachments) != 0 || !strings.Contains(notice, "transcript words") || strings.Contains(notice, "失败") {
		t.Fatalf("%+v %s", attachments, notice)
	}
	items := make([]client.ClawBotIncomingMessageItem, 17)
	for i := range items {
		items[i] = client.ClawBotIncomingMessageItem{Type: 4, FileItem: &client.ClawBotFileItem{FileName: "missing.bin"}}
	}
	_, notice = s.receiveClawBotAttachments(context.Background(), client.ClawBotIncomingMessage{ItemList: items})
	if strings.Count(notice, "内容不可用") != 16 || !strings.Contains(notice, "超限") {
		t.Fatal(notice)
	}
}
