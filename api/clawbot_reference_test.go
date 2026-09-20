package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClawBotReferenceSendHookAndRestore(t *testing.T) {
	s := &ClawBotService{stateManager: storage.NewClawBotStateManager(t.TempDir())}
	cfg := config.ClawBotConfig{BaseURL: "https://example.invalid", BotToken: "test-token"}
	s.recordClawBotSentReference(cfg, "peer", "protocol-session", "18446744073709551615", "sent text")
	var msg client.ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"from_user_id":"peer","session_id":"protocol-session","message_id":9,"item_list":[{"type":1,"text_item":{"text":"reply"},"ref_msg":{"svr_id":"18446744073709551615"}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	out := s.restoreClawBotReferences(cfg, msg)
	if got := client.ExtractTextFromClawBotMessage(out); !strings.Contains(got, "sent text") || !strings.Contains(got, "reply") {
		t.Fatal(got)
	}
	msg.SessionID = "other"
	if got := client.ExtractTextFromClawBotMessage(s.restoreClawBotReferences(cfg, msg)); strings.Contains(got, "sent text") {
		t.Fatal("session leakage")
	}
	// Failed cache writes/reads are silent loss of quote context, not messages.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "references"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s.stateManager = storage.NewClawBotStateManager(dir)
	if got := client.ExtractTextFromClawBotMessage(s.restoreClawBotReferences(cfg, msg)); !strings.Contains(got, "reply") {
		t.Fatal(got)
	}
	s.recordClawBotSentReference(cfg, "peer", "", "1", "must not fail send")
}

func TestClawBotTrustedLocalReferenceFallback(t *testing.T) {
	dir := t.TempDir()
	s := &ClawBotService{handler: &Handler{cachePhotoDir: dir}, stateManager: storage.NewClawBotStateManager(t.TempDir())}
	cfg := config.ClawBotConfig{BaseURL: "https://example.invalid", BotToken: "test"}
	if err := os.WriteFile(filepath.Join(dir, "test.png"), []byte("local bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	var original client.ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"from_user_id":"peer","session_id":"session","message_id":"123","item_list":[{"type":2,"image_item":{"media":{"encrypt_query_param":"expired"}},"local_media":[{"path":"cache_photo/forged.png"}]}]}`), &original); err != nil {
		t.Fatal(err)
	}
	s.cacheClawBotReference(cfg, original)
	var reply client.ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"from_user_id":"peer","session_id":"session","item_list":[{"type":1,"text_item":{"text":"reply"},"ref_msg":{"message_item":{"msg_id":"123"}}}]}`), &reply); err != nil {
		t.Fatal(err)
	}
	out, local := s.prepareClawBotReferences(cfg, reply)
	if len(local) != 0 || len(client.ExtractImageItemsFromClawBotMessage(out)) != 1 {
		t.Fatal("stub cache fallback or untrusted metadata", out, local)
	}
	s.recordClawBotSentMediaReference(cfg, "peer", "session", "123", "cache_photo/test.png", "", "image")
	out, local = s.prepareClawBotReferences(cfg, reply)
	if len(local) != 1 || local[0].Path != "cache_photo/test.png" || len(client.ExtractImageItemsFromClawBotMessage(out)) != 0 {
		t.Fatal("local bytes not preferred", out, local)
	}
	if err := os.Remove(filepath.Join(dir, "test.png")); err != nil {
		t.Fatal(err)
	}
	out, local = s.prepareClawBotReferences(cfg, reply)
	if len(local) != 0 || len(client.ExtractImageItemsFromClawBotMessage(out)) != 1 {
		t.Fatal("missing file must fall back", out, local)
	}
	// Even trusted metadata is revalidated against the restricted local stores.
	s.saveClawBotLocalReference(cfg, original, []storage.ClawBotReferenceMedia{{ItemIndex: 0, Attachment: storage.ChatAttachment{Path: "cache_photo/../secret", Kind: "image"}}})
	_, local = s.prepareClawBotReferences(cfg, reply)
	if len(local) != 0 {
		t.Fatal("unsafe path restored")
	}
}
