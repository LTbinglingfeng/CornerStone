package api

import (
	"bytes"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestConfigManagerWithMemoryProvider(t *testing.T, provider config.Provider) *config.Manager {
	t.Helper()

	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.MemoryProvider = &provider
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}
	return cm
}

func TestHandleProviderByID_PromptCacheSettingsPreservedWhenOmitted(t *testing.T) {
	provider := config.DefaultProvider()
	provider.ID = "provider-1"
	provider.Name = "Test Anthropic"
	provider.Type = config.ProviderTypeAnthropic
	provider.BaseURL = "https://api.example.com/v1"
	provider.APIKey = "secret-key"
	provider.Model = "claude-test"
	provider.Stream = true
	provider.ImageCapable = false
	provider.PromptCaching = true
	provider.PromptCacheTTL = "1h"

	handler := &Handler{configManager: newTestProviderConfigManager(t, provider)}

	body, err := json.Marshal(map[string]interface{}{
		"name":          "Test Anthropic Updated",
		"type":          "anthropic",
		"model":         "claude-test-new",
		"stream":        true,
		"image_capable": false,
		// prompt_caching / prompt_cache_ttl intentionally omitted
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/providers/provider-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleProviderByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated := handler.configManager.GetProvider("provider-1")
	if updated == nil {
		t.Fatalf("provider not found after update")
	}
	if updated.PromptCaching != true {
		t.Fatalf("PromptCaching = %v, want %v", updated.PromptCaching, true)
	}
	if updated.PromptCacheTTL != "1h" {
		t.Fatalf("PromptCacheTTL = %q, want %q", updated.PromptCacheTTL, "1h")
	}
}

func TestHandleProviderByID_PromptCachingCanDisableExplicitly(t *testing.T) {
	provider := config.DefaultProvider()
	provider.ID = "provider-1"
	provider.Name = "Test Anthropic"
	provider.Type = config.ProviderTypeAnthropic
	provider.BaseURL = "https://api.example.com/v1"
	provider.APIKey = "secret-key"
	provider.Model = "claude-test"
	provider.Stream = true
	provider.ImageCapable = false
	provider.PromptCaching = true
	provider.PromptCacheTTL = "1h"

	handler := &Handler{configManager: newTestProviderConfigManager(t, provider)}

	body, err := json.Marshal(map[string]interface{}{
		"name":           "Test Anthropic Updated",
		"type":           "anthropic",
		"model":          "claude-test-new",
		"stream":         true,
		"image_capable":  false,
		"prompt_caching": false,
		// prompt_cache_ttl intentionally omitted (keep existing)
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/providers/provider-1", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleProviderByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated := handler.configManager.GetProvider("provider-1")
	if updated == nil {
		t.Fatalf("provider not found after update")
	}
	if updated.PromptCaching != false {
		t.Fatalf("PromptCaching = %v, want %v", updated.PromptCaching, false)
	}
	if updated.PromptCacheTTL != "1h" {
		t.Fatalf("PromptCacheTTL = %q, want %q", updated.PromptCacheTTL, "1h")
	}
}

func TestHandleMemoryProvider_PromptCacheSettingsPreservedWhenOmitted(t *testing.T) {
	provider := config.DefaultProvider()
	provider.ID = "memory"
	provider.Name = "Memory Provider"
	provider.Type = config.ProviderTypeAnthropic
	provider.BaseURL = "https://api.example.com/v1"
	provider.APIKey = "secret-key"
	provider.Model = "claude-test"
	provider.Stream = true
	provider.ImageCapable = false
	provider.PromptCaching = true
	provider.PromptCacheTTL = "1h"

	handler := &Handler{configManager: newTestConfigManagerWithMemoryProvider(t, provider)}

	body, err := json.Marshal(map[string]interface{}{
		"use_custom": true,
		"provider": map[string]interface{}{
			"type":          "anthropic",
			"model":         "claude-test-new",
			"stream":        true,
			"image_capable": false,
			// prompt_caching / prompt_cache_ttl intentionally omitted
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/memory-provider", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleMemoryProvider(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	cfg := handler.configManager.Get()
	if cfg.MemoryProvider == nil {
		t.Fatalf("expected memory provider after update")
	}
	if cfg.MemoryProvider.PromptCaching != true {
		t.Fatalf("PromptCaching = %v, want %v", cfg.MemoryProvider.PromptCaching, true)
	}
	if cfg.MemoryProvider.PromptCacheTTL != "1h" {
		t.Fatalf("PromptCacheTTL = %q, want %q", cfg.MemoryProvider.PromptCacheTTL, "1h")
	}
}

func TestHandleMemoryProvider_PromptCachingCanDisableExplicitly(t *testing.T) {
	provider := config.DefaultProvider()
	provider.ID = "memory"
	provider.Name = "Memory Provider"
	provider.Type = config.ProviderTypeAnthropic
	provider.BaseURL = "https://api.example.com/v1"
	provider.APIKey = "secret-key"
	provider.Model = "claude-test"
	provider.Stream = true
	provider.ImageCapable = false
	provider.PromptCaching = true
	provider.PromptCacheTTL = "1h"

	handler := &Handler{configManager: newTestConfigManagerWithMemoryProvider(t, provider)}

	body, err := json.Marshal(map[string]interface{}{
		"use_custom": true,
		"provider": map[string]interface{}{
			"type":           "anthropic",
			"model":          "claude-test-new",
			"stream":         true,
			"image_capable":  false,
			"prompt_caching": false,
			// prompt_cache_ttl intentionally omitted (keep existing)
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/memory-provider", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleMemoryProvider(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	cfg := handler.configManager.Get()
	if cfg.MemoryProvider == nil {
		t.Fatalf("expected memory provider after update")
	}
	if cfg.MemoryProvider.PromptCaching != false {
		t.Fatalf("PromptCaching = %v, want %v", cfg.MemoryProvider.PromptCaching, false)
	}
	if cfg.MemoryProvider.PromptCacheTTL != "1h" {
		t.Fatalf("PromptCacheTTL = %q, want %q", cfg.MemoryProvider.PromptCacheTTL, "1h")
	}
}
