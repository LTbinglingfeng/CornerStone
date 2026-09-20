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
