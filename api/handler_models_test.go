package api

import (
	"bytes"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestFetchModelsFromProvider_GeminiBaseURLWithoutVersion(t *testing.T) {
	var gotPath, gotKeyHeader, gotKeyQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKeyHeader = r.Header.Get("x-goog-api-key")
		gotKeyQuery = r.URL.Query().Get("key")
		if r.URL.Path != "/v1beta/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-test"}]}`))
	}))
	defer server.Close()

	for _, baseURL := range []string{server.URL, server.URL + "/", server.URL + "/v1beta"} {
		models, err := fetchModelsFromProvider(&config.Provider{
			Type:    config.ProviderTypeGemini,
			BaseURL: baseURL,
			APIKey:  "gemini-key",
		})
		if err != nil {
			t.Fatalf("base %q: unexpected error: %v", baseURL, err)
		}
		if len(models) != 1 || models[0] != "gemini-test" {
			t.Fatalf("base %q: unexpected models %v", baseURL, models)
		}
		if gotPath != "/v1beta/models" || gotKeyHeader != "gemini-key" || gotKeyQuery != "" {
			t.Fatalf("base %q: path=%q header=%q query=%q", baseURL, gotPath, gotKeyHeader, gotKeyQuery)
		}
	}
}

func TestFetchModelsFromProvider_GeminiPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/b"}]}`))
	}))
	defer server.Close()

	models, err := fetchModelsFromProvider(&config.Provider{Type: config.ProviderTypeGemini, BaseURL: server.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 || models[0] != "a" || models[1] != "b" {
		t.Fatalf("unexpected models %v", models)
	}
}

func TestFetchModelsFromProvider_NotFoundErrorIsReadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := fetchModelsFromProvider(&config.Provider{Type: config.ProviderTypeOpenAI, BaseURL: server.URL, APIKey: "k"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), server.URL+"/models") || !strings.Contains(err.Error(), "API URL") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
