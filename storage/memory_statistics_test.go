package storage

import (
	"cornerstone/client"
	"cornerstone/config"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStatisticsRecorder struct {
	mu      sync.Mutex
	records []client.CallRecord
}

func (r *memoryStatisticsRecorder) RecordCall(record client.CallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func TestMemoryStatisticsExtractAndSaveIntegration(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"memory-fixture","object":"chat.completion","model":"memory-statistics-test","choices":[{"index":0,"message":{"role":"assistant","content":"[]"},"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"completion_tokens":3,"total_tokens":20}}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	cm := config.NewManager(filepath.Join(dir, "config.json"))
	cfg := config.DefaultConfig()
	cfg.MemoryEnabled = true
	cfg.MemoryProvider = &config.Provider{ID: "memory-statistics-provider", Type: config.ProviderTypeOpenAI, BaseURL: server.URL + "/v1", APIKey: "synthetic-key", Model: "memory-statistics-test"}
	if err := cm.Update(cfg); err != nil {
		t.Fatal(err)
	}
	promptDir := filepath.Join(dir, "prompts", "fixture")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.json"), []byte(`{"id":"fixture","name":"Fixture","content":"Synthetic test persona"}`), 0644); err != nil {
		t.Fatal(err)
	}
	chats := NewChatManager(filepath.Join(dir, "chats"))
	if _, err := chats.CreateSession("fixture-session", "Fixture", "fixture", "Fixture"); err != nil {
		t.Fatal(err)
	}
	if err := chats.AddMessage("fixture-session", "user", "Synthetic greeting"); err != nil {
		t.Fatal(err)
	}
	if err := chats.AddMessage("fixture-session", "assistant", "Synthetic reply"); err != nil {
		t.Fatal(err)
	}
	extractor := NewMemoryExtractor(NewMemoryManager(filepath.Join(dir, "prompts")), cm, chats, NewUserManager(filepath.Join(dir, "user")), "")
	recorder := &memoryStatisticsRecorder{}
	extractor.SetCallRecorder(recorder)
	before := time.Now()
	if err := extractor.ExtractAndSave("fixture", "fixture-session"); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if calls.Load() != 1 || len(recorder.records) != 1 {
		t.Fatalf("upstream=%d recorded=%d, want one each", calls.Load(), len(recorder.records))
	}
	record := recorder.records[0]
	if record.ProviderID != cfg.MemoryProvider.ID || record.Model != cfg.MemoryProvider.Model || !record.Success || !record.UsageReported || record.InputTokens != 17 || record.OutputTokens != 3 || record.TotalTokens != 20 {
		t.Fatalf("record = %+v", record)
	}
	if record.CompletedAt.Before(before) || record.CompletedAt.After(after) || record.DurationMS < 0 {
		t.Fatalf("invalid timing: %+v", record)
	}
}
