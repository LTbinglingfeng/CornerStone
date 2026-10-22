package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeminiAutomaticContinuationPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// Legacy configuration has no continuation field, including the custom memory provider.
	if err := os.WriteFile(path, []byte(`{"providers":[{"id":"gemini","type":"gemini"}],"memory_provider":{"type":"gemini"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path)
	if manager.GetProvider("gemini").GeminiAutomaticContinuation || manager.Get().MemoryProvider.GeminiAutomaticContinuation || DefaultProvider().GeminiAutomaticContinuation {
		t.Fatal("legacy/default continuation enabled")
	}
	for _, enabled := range []bool{true, false} {
		cfg := manager.Get()
		cfg.Providers[0].GeminiAutomaticContinuation = enabled
		memory := *cfg.MemoryProvider
		memory.GeminiAutomaticContinuation = enabled
		cfg.MemoryProvider = &memory
		if err := manager.Update(cfg); err != nil {
			t.Fatal(err)
		}
		manager = NewManager(path)
		if manager.GetProvider("gemini").GeminiAutomaticContinuation != enabled || manager.Get().MemoryProvider.GeminiAutomaticContinuation != enabled {
			t.Fatal("continuation not persisted")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Providers []map[string]any `json:"providers"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Providers[0]["gemini_automatic_continuation"] != enabled {
			t.Fatalf("missing explicit bool: %s", data)
		}
	}
	cfg := manager.Get()
	cfg.Providers[0].Type = ProviderTypeOpenAI
	cfg.Providers[0].GeminiAutomaticContinuation = true
	memory := *cfg.MemoryProvider
	memory.Type = ProviderTypeAnthropic
	memory.GeminiAutomaticContinuation = true
	cfg.MemoryProvider = &memory
	if err := manager.Update(cfg); err != nil {
		t.Fatal(err)
	}
	manager = NewManager(path)
	if manager.GetProvider("gemini").GeminiAutomaticContinuation || manager.Get().MemoryProvider.GeminiAutomaticContinuation {
		t.Fatal("non-Gemini setting not cleared")
	}
	p := *manager.GetProvider("gemini")
	p.GeminiAutomaticContinuation = true
	if err := manager.UpdateProvider(p); err != nil {
		t.Fatal(err)
	}
	if manager.GetProvider("gemini").GeminiAutomaticContinuation {
		t.Fatal("UpdateProvider retained non-Gemini setting")
	}
	p.ID = "new"
	if err := manager.AddProvider(p); err != nil {
		t.Fatal(err)
	}
	if manager.GetProvider("new").GeminiAutomaticContinuation {
		t.Fatal("AddProvider retained non-Gemini setting")
	}
}
