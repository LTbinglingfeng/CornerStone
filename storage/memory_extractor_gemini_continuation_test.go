package storage

import (
	"cornerstone/config"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestMemoryExtractorGeminiAutomaticContinuation(t *testing.T) {
	for _, source := range []string{"custom", "provider-id", "active"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%v", source, enabled), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hop := calls.Add(1)
					if r.URL.Path != "/v1beta/models/fixture-model:generateContent" {
						t.Errorf("unexpected path: %s", r.URL.Path)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if hop == 1 && body["continuationToken"] != nil {
						t.Error("unexpected initial token")
					}
					if hop == 2 && body["continuationToken"] != "dG9rZW4=" {
						t.Errorf("continuation token = %v", body["continuationToken"])
					}
					w.Header().Set("Content-Type", "application/json")
					if hop == 1 {
						fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ignored"}]},"finishReason":"CONTINUATION","continuationToken":"dG9rZW4="}]}`)
					} else {
						fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"memory_batch_upsert","args":{"items":[]}}}]},"finishReason":"STOP"}]}`)
					}
				}))
				defer server.Close()
				root := t.TempDir()
				cm := config.NewManager(filepath.Join(root, "config.json"))
				cfg := config.DefaultConfig()
				cfg.MemoryEnabled = true
				provider := config.DefaultProvider()
				provider.ID = "fixture"
				provider.Type = config.ProviderTypeGemini
				provider.Model = "fixture-model"
				provider.APIKey = "fixture-key"
				provider.BaseURL = server.URL
				provider.GeminiAutomaticContinuation = enabled
				switch source {
				case "custom":
					cfg.MemoryProvider = &provider
				case "provider-id":
					cfg.Providers = []config.Provider{provider}
					cfg.MemoryProviderID = provider.ID
				case "active":
					cfg.Providers = []config.Provider{provider}
					cfg.ActiveProviderID = provider.ID
				}
				if err := cm.Update(cfg); err != nil {
					t.Fatal(err)
				}
				promptDir := filepath.Join(root, "prompts", "prompt_1")
				if err := os.MkdirAll(promptDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(promptDir, "prompt.json"), []byte(`{"id":"prompt_1","name":"fixture","content":"fixture"}`), 0600); err != nil {
					t.Fatal(err)
				}
				chats := NewChatManager(filepath.Join(root, "chats"))
				if _, err := chats.CreateSession("session_1", "fixture", "prompt_1", "fixture"); err != nil {
					t.Fatal(err)
				}
				if err := chats.AddMessage("session_1", "user", "hello"); err != nil {
					t.Fatal(err)
				}
				if err := chats.AddMessage("session_1", "assistant", "hello"); err != nil {
					t.Fatal(err)
				}
				extractor := NewMemoryExtractor(NewMemoryManager(filepath.Join(root, "prompts")), cm, chats, NewUserManager(filepath.Join(root, "user")), "")
				if err := extractor.ExtractAndSave("prompt_1", "session_1"); err != nil {
					t.Fatal(err)
				}
				want := int32(1)
				if enabled {
					want = 2
				}
				if calls.Load() != want {
					t.Fatalf("requests=%d, want %d", calls.Load(), want)
				}
			})
		}
	}
}
