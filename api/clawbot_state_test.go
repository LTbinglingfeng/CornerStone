package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClawBotStateRestoresCursorAndContext(t *testing.T) {
	dir := t.TempDir()
	s := newTestClawBotService(t, "https://ilinkai.weixin.qq.com")
	s.stateManager = storage.NewClawBotStateManager(dir)
	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.GetUpdatesBuf = "legacy-cursor"
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if s.pollCursor(cfg) != "legacy-cursor" {
		t.Fatal("legacy cursor not seeded")
	}
	msg := client.ClawBotIncomingMessage{MessageType: 1, MessageID: "18446744073709551615", FromUserID: "user", ContextToken: "ctx-token"}
	if err := s.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	if err := s.savePollCheckpoint(cfg, "new-cursor", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	s.activeSessions["user"] = &clawBotActiveSession{LastActive: time.Now().Add(-time.Hour)}
	s.cleanupState()
	if s.getContextToken("user") != "ctx-token" {
		t.Fatal("idle cleanup removed reply context")
	}

	restarted := newTestClawBotService(t, cfg.BaseURL)
	restarted.stateManager = storage.NewClawBotStateManager(dir)
	if err := restarted.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if restarted.pollCursor(cfg) != "new-cursor" || restarted.getContextToken("user") != "ctx-token" || !restarted.alreadyReceived(cfg, msg) {
		t.Fatal("restart lost channel state")
	}
	cfg.BotToken = "other-token"
	cfg.GetUpdatesBuf = ""
	if err := restarted.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := restarted.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if restarted.pollCursor(cfg) != "" || restarted.getContextToken("user") != "" || restarted.alreadyReceived(cfg, msg) {
		t.Fatal("account state leaked across credentials")
	}
}

func TestClawBotCheckpointFailureDoesNotAdvanceMemory(t *testing.T) {
	dir := t.TempDir()
	s := newTestClawBotService(t, "https://ilinkai.weixin.qq.com")
	s.stateManager = storage.NewClawBotStateManager(dir)
	cfg := s.handler.configManager.GetClawBotConfig()
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.savePollCheckpoint(cfg, "old", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, clawBotAccount(cfg)+".json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.savePollCheckpoint(cfg, "new", nil); err == nil {
		t.Fatal("expected persistence failure")
	}
	if s.pollCursor(cfg) != "old" {
		t.Fatal("failed checkpoint advanced memory")
	}
	if data, _ := os.ReadFile(path); string(data) != "broken" {
		t.Fatal("corrupt state overwritten")
	}
}

func TestClawBotPollPersistsAndDeduplicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls, sends := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/sendmessage" {
			sends++
			fmt.Fprint(w, `{"ret":0}`)
			return
		}
		var req client.ClawBotGetUpdatesRequest
		json.NewDecoder(r.Body).Decode(&req)
		polls++
		if polls > 1 && req.GetUpdatesBuf != "cursor" {
			t.Error("cursor not passed back")
		}
		if polls == 3 {
			cancel()
			return
		}
		fmt.Fprint(w, `{"get_updates_buf":"cursor","msgs":[{"message_id":18446744073709551615,"message_type":1,"from_user_id":"user","context_token":"context","item_list":[{"type":1,"text_item":{"text":"/menu"}}]}]}`)
	}))
	defer server.Close()
	s := newTestClawBotService(t, server.URL)
	dir := t.TempDir()
	s.stateManager = storage.NewClawBotStateManager(dir)
	cfg := s.handler.configManager.GetClawBotConfig()
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	s.pollLoop(ctx, cfg)
	if sends != 0 {
		t.Fatal("poll executed a command before durable queue dispatch")
	}
	state, err := s.stateManager.Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if state.GetUpdatesBuf != "cursor" || state.ContextTokens["user"] != "context" || state.Delivery == nil || len(state.Delivery.Jobs) != 1 {
		t.Fatal("poll state not saved")
	}
	id := state.Delivery.Jobs[0].ID
	for range 3 {
		job, _ := s.snapshotDelivery(id)
		s.stepDelivery(context.Background(), cfg, job)
	}
	if sends != 1 {
		t.Fatalf("dispatch sent %d messages", sends)
	}
}

func TestClawBotReplayDoesNotRollBackContext(t *testing.T) {
	s := newTestClawBotService(t, "https://ilinkai.weixin.qq.com")
	s.stateManager = storage.NewClawBotStateManager(t.TempDir())
	cfg := s.handler.configManager.GetClawBotConfig()
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	old := client.ClawBotIncomingMessage{MessageType: 1, MessageID: "1", FromUserID: "user", ContextToken: "A"}
	latest := client.ClawBotIncomingMessage{MessageType: 1, MessageID: "2", FromUserID: "user", ContextToken: "B"}
	if err := s.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{latest}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{old, latest}); err != nil {
		t.Fatal(err)
	}
	if s.getContextToken("user") != "B" {
		t.Fatal("batch comparison rolled context back")
	}
	if err := s.savePollCheckpoint(cfg, "cursor", []client.ClawBotIncomingMessage{old, latest}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{old}); err != nil {
		t.Fatal(err)
	}
	if s.getContextToken("user") != "B" {
		t.Fatal("duplicate rolled context back")
	}
}

func TestClawBotMessageIDLossless(t *testing.T) {
	for _, raw := range []string{`{"message_id":18446744073709551615}`, `{"message_id":"18446744073709551615"}`} {
		var msg client.ClawBotIncomingMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			t.Fatal(err)
		}
		if msg.MessageID != "18446744073709551615" {
			t.Fatal("message ID lost precision")
		}
	}
}
