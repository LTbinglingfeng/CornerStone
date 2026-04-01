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

func newTestProviderConfigManager(t *testing.T, provider config.Provider) *config.Manager {
	t.Helper()

	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.Providers = []config.Provider{provider}
	cfg.ActiveProviderID = provider.ID
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}
	return cm
}

func newTestProvider(id string) config.Provider {
	provider := config.DefaultProvider()
	provider.ID = id
	provider.Name = "Test Provider"
	provider.Type = config.ProviderTypeOpenAI
	provider.BaseURL = "https://api.example.com/v1"
	provider.APIKey = "secret-key"
	provider.Model = "gpt-test"
	return provider
}

func TestCanReuseSavedModelFetchAPIKey(t *testing.T) {
	saved := &config.Provider{
		Type:    config.ProviderTypeOpenAI,
		BaseURL: "https://api.example.com/v1/",
		APIKey:  "secret-key",
	}

	if !canReuseSavedModelFetchAPIKey(saved, modelsRequest{
		Type:    "openai",
		BaseURL: "https://api.example.com/v1",
	}) {
		t.Fatal("expected API key reuse when type and base URL still match")
	}

	if canReuseSavedModelFetchAPIKey(saved, modelsRequest{
		Type:    "openai",
		BaseURL: "https://evil.example.com/v1",
	}) {
		t.Fatal("expected API key reuse to be blocked after base URL edit")
	}

	if canReuseSavedModelFetchAPIKey(saved, modelsRequest{
		Type:    "anthropic",
		BaseURL: "https://api.example.com/v1",
	}) {
		t.Fatal("expected API key reuse to be blocked after provider type edit")
	}
}

func TestHandleProviderModels_RequiresExplicitKeyForEditedEndpoint(t *testing.T) {
	provider := newTestProvider("provider-1")
	handler := &Handler{configManager: newTestProviderConfigManager(t, provider)}

	body, err := json.Marshal(modelsRequest{
		Type:    "openai",
		BaseURL: "https://evil.example.com/v1",
		APIKey:  "",
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/management/providers/provider-1/models", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleProviderModels(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode response failed: %v", err)
	}
	if resp.Error != "api_key is required" {
		t.Fatalf("error = %q, want %q", resp.Error, "api_key is required")
	}
}
