package storage

import (
	"cornerstone/config"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryExtractor_ExtractAndSave_ForwardsAnthropicPromptCachingSettings(t *testing.T) {
	bodyCh := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		select {
		case bodyCh <- string(body):
		default:
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"[]"}],"stop_reason":"end_turn"}`)
	}))
	defer server.Close()

	tempDir := t.TempDir()

	cm := config.NewManager(filepath.Join(tempDir, "config.json"))
	cfg := config.DefaultConfig()
	cfg.MemoryEnabled = true
	cfg.MemoryProvider = &config.Provider{
		ID:             "memory",
		Name:           "Memory Provider",
		Type:           config.ProviderTypeAnthropic,
		BaseURL:        server.URL,
		APIKey:         "secret-key",
		Model:          "claude-test",
		Stream:         false,
		ImageCapable:   false,
		PromptCaching:  true,
		PromptCacheTTL: "1h",
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	memoryDir := filepath.Join(tempDir, "prompts")
	mm := NewMemoryManager(memoryDir)

	promptID := "prompt_1"
	promptDir := filepath.Join(memoryDir, promptID)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.json"), []byte(`{"id":"prompt_1","name":"小助手","content":"你是一个测试角色。"}`), 0644); err != nil {
		t.Fatalf("write prompt.json: %v", err)
	}

	chatMgr := NewChatManager(filepath.Join(tempDir, "chats"))
	sessionID := "session_1"
	if _, err := chatMgr.CreateSession(sessionID, "test", promptID, "小助手"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chatMgr.AddMessage(sessionID, "user", "你好"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if err := chatMgr.AddMessage(sessionID, "assistant", "你好呀"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	userMgr := NewUserManager(filepath.Join(tempDir, "user"))

	extractor := NewMemoryExtractor(mm, cm, chatMgr, userMgr, "")
	if err := extractor.ExtractAndSave(promptID, sessionID); err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	var gotRequestBody string
	select {
	case gotRequestBody = <-bodyCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for upstream request body")
	}
	if !strings.Contains(gotRequestBody, "\"cache_control\"") {
		t.Fatalf("expected cache_control in request body, got: %s", gotRequestBody)
	}
	if !strings.Contains(gotRequestBody, "\"ttl\":\"1h\"") {
		t.Fatalf("expected ttl=1h in request body, got: %s", gotRequestBody)
	}
}
