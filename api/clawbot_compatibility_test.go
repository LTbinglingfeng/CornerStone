package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClawBotItemIDReferenceAndMedia(t *testing.T) {
	photos := t.TempDir()
	s := &ClawBotService{handler: &Handler{cachePhotoDir: photos}, stateManager: storage.NewClawBotStateManager(t.TempDir())}
	cfg := config.ClawBotConfig{BaseURL: "https://example.invalid", BotToken: "test-token"}
	if err := os.WriteFile(filepath.Join(photos, "test.png"), []byte("local bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	var original client.ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"from_user_id":"peer","session_id":"session","item_list":[{"msg_id":18446744073709551615,"type":1,"text_item":{"text":"original quoted text"}},{"type":2,"image_item":{"media":{"encrypt_query_param":"expired"}}}]}`), &original); err != nil {
		t.Fatal(err)
	}
	s.cacheClawBotReference(cfg, original)
	s.saveClawBotLocalReference(cfg, original, []storage.ClawBotReferenceMedia{{ItemIndex: 1, Attachment: storage.ChatAttachment{Path: "cache_photo/test.png", Kind: "image"}}})
	var reply client.ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"from_user_id":"peer","session_id":"session","item_list":[{"type":1,"text_item":{"text":"reply"},"ref_msg":{"svr_id":18446744073709551615}}]}`), &reply); err != nil {
		t.Fatal(err)
	}
	out, local := s.prepareClawBotReferences(cfg, reply)
	if !strings.Contains(client.ExtractTextFromClawBotMessage(out), "original quoted text") || len(local) != 1 || local[0].Path != "cache_photo/test.png" || len(client.ExtractImageItemsFromClawBotMessage(out)) != 0 {
		t.Fatalf("item ID reference not restored: %+v, %+v", out, local)
	}
	for _, scope := range []string{"session", "peer", "account"} {
		t.Run(scope+" isolation", func(t *testing.T) {
			other, otherConfig := reply, cfg
			switch scope {
			case "session":
				other.SessionID = "other-session"
			case "peer":
				other.FromUserID = "other-peer"
			case "account":
				otherConfig.BotToken = "other-token"
			}
			out, local := s.prepareClawBotReferences(otherConfig, other)
			if strings.Contains(client.ExtractTextFromClawBotMessage(out), "original quoted text") || len(local) != 0 {
				t.Fatal("reference crossed isolation boundary")
			}
		})
	}
}

func TestClawBotItemIDCheckpointAndOpaqueContext(t *testing.T) {
	s, cfg, dir := deliveryTestService(t, "https://example.invalid")
	token := " \topaque-context\r\n"
	msg := newClawBotTextIncomingMessage("user", "hello")
	msg.ItemList[0].MsgID = "18446744073709551615"
	msg.ContextToken = token
	if err := s.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	if err := s.savePollCheckpoint(cfg, "cursor", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	restarted := newTestClawBotService(t, cfg.BaseURL)
	restarted.stateManager = storage.NewClawBotStateManager(dir)
	if err := restarted.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if !restarted.alreadyReceived(cfg, msg) || restarted.getContextToken("user") != token {
		t.Fatal("restart lost item ID receipt or changed opaque context")
	}
	msg.ContextToken = "replayed-token"
	if err := restarted.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	if restarted.getContextToken("user") != token {
		t.Fatal("replay overwrote latest context")
	}
	// An absent token must not erase the latest one; whitespace is opaque data.
	for _, next := range []string{"", " \t\r\n"} {
		msg.ItemList[0].MsgID = "123"
		msg.ContextToken = next
		if err := restarted.saveIncomingContextTokens(cfg, []client.ClawBotIncomingMessage{msg}); err != nil {
			t.Fatal(err)
		}
		if next != "" {
			token = next
		}
		if restarted.getContextToken("user") != token {
			t.Fatal("opaque context changed")
		}
	}
}

func TestClawBotItemIDQueueReplayAndOpaqueContext(t *testing.T) {
	for _, token := range []string{" \topaque-context\r\n", " \t\r\n"} {
		t.Run(fmt.Sprintf("token=%q", token), func(t *testing.T) {
			testClawBotItemIDQueueReplayAndOpaqueContext(t, token)
		})
	}
}

func testClawBotItemIDQueueReplayAndOpaqueContext(t *testing.T, token string) {
	t.Helper()
	sends := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body client.ClawBotSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Msg.ContextToken != token {
			t.Errorf("sent token = %q, want %q", body.Msg.ContextToken, token)
		}
		sends++
		fmt.Fprint(w, `{"ret":0,"message_id":"321"}`)
	}))
	defer server.Close()
	s, cfg, dir := deliveryTestService(t, server.URL)
	msg := newClawBotTextIncomingMessage("user", "/menu")
	msg.ItemList[0].MsgID = "18446744073709551615"
	msg.ContextToken = token
	if err := s.receiveDeliveryBatch(cfg, "old", "cursor-1", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	// The same item-only ID across a new cursor must not enqueue another reply
	// or replace the saved token, including after durable state is reloaded.
	s.stateManager = storage.NewClawBotStateManager(dir)
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	msg.ContextToken = "replayed-token"
	if err := s.receiveDeliveryBatch(cfg, "cursor-1", "cursor-2", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	disk, err := s.stateManager.Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if disk.GetUpdatesBuf != "cursor-2" || disk.ContextTokens["user"] != token || disk.Delivery == nil || len(disk.Delivery.Jobs) != 1 || len(disk.Delivery.Receipts) != 1 || len(disk.Delivery.Jobs[0].Inputs) != 1 {
		t.Fatalf("replay changed durable state: %+v", disk)
	}
	if disk.Delivery.Receipts["user:18446744073709551615"] != disk.Delivery.Jobs[0].ID {
		t.Fatal("receipt did not use lossless item ID")
	}
	msg.ItemList[0].MsgID = "123"
	msg.ContextToken = ""
	if err := s.receiveDeliveryBatch(cfg, "cursor-2", "cursor-3", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	s.stateManager = storage.NewClawBotStateManager(dir)
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	disk, err = s.stateManager.Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if disk.ContextTokens["user"] != token || len(disk.Delivery.Jobs) != 2 {
		t.Fatal("new message with no token erased saved context")
	}
	for _, job := range disk.Delivery.Jobs {
		for range 3 {
			deliveryTestStep(t, s, cfg, job.ID)
		}
	}
	if sends != 2 {
		t.Fatalf("sent %d replies, want 2", sends)
	}
}

func TestClawBotMissingMessageIDKeepsFallback(t *testing.T) {
	for _, id := range []client.ClawBotMessageID{"", "0"} {
		t.Run(fmt.Sprintf("ID=%q", id), func(t *testing.T) {
			s, cfg, dir := deliveryTestService(t, "https://example.invalid")
			msg := newClawBotTextIncomingMessage("user", "/menu")
			msg.MessageID, msg.ItemList[0].MsgID = id, id
			if key := clawBotMessageKey(msg); key != "" {
				t.Fatalf("invalid stable key: %q", key)
			}
			s.cacheClawBotReference(cfg, msg)
			s.saveClawBotLocalReference(cfg, msg, []storage.ClawBotReferenceMedia{{ItemIndex: 0, Attachment: storage.ChatAttachment{Path: "cache_photo/test.png", Kind: "image"}}})
			if _, err := os.Stat(filepath.Join(dir, "references")); !os.IsNotExist(err) {
				t.Fatalf("created reference cache without an ID: %v", err)
			}
			for range 2 {
				if err := s.receiveDeliveryBatch(cfg, "old", "next", []client.ClawBotIncomingMessage{msg}); err != nil {
					t.Fatal(err)
				}
			}
			disk, err := s.stateManager.Load(clawBotAccount(cfg))
			if err != nil {
				t.Fatal(err)
			}
			if len(disk.Delivery.Jobs) != 1 || len(disk.Delivery.Receipts) != 1 {
				t.Fatal("same-batch fallback deduplication changed")
			}
			for key := range disk.Delivery.Receipts {
				if !strings.HasPrefix(key, "fallback:") {
					t.Fatalf("invalid fallback key: %q", key)
				}
			}
		})
	}
}
