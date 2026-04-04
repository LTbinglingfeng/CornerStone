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

func TestHandleWebSearchSettings_CanClearSecretsWithExplicitEmptyStrings(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WebSearch.ActiveProviderID = "tavily"
	cfg.WebSearch.Providers = map[string]config.WebSearchProvider{
		"tavily": {
			APIKey:            "secret-key",
			APIHost:           "https://api.tavily.com",
			BasicAuthUsername: "alice",
			BasicAuthPassword: "secret-password",
		},
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	handler := &Handler{configManager: cm}
	body, err := json.Marshal(map[string]interface{}{
		"fetch_results": 12,
		"providers": map[string]interface{}{
			"tavily": map[string]string{
				"api_key":             "",
				"basic_auth_password": "",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/web-search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleWebSearchSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := cm.Get()
	providerCfg, ok := updated.WebSearch.Providers["tavily"]
	if !ok {
		t.Fatalf("provider settings missing after update")
	}
	if providerCfg.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty", providerCfg.APIKey)
	}
	if providerCfg.BasicAuthPassword != "" {
		t.Fatalf("BasicAuthPassword = %q, want empty", providerCfg.BasicAuthPassword)
	}
	if providerCfg.APIHost != "https://api.tavily.com" {
		t.Fatalf("APIHost = %q, want preserved", providerCfg.APIHost)
	}
	if providerCfg.BasicAuthUsername != "alice" {
		t.Fatalf("BasicAuthUsername = %q, want preserved", providerCfg.BasicAuthUsername)
	}
	if updated.WebSearch.FetchResults != 12 {
		t.Fatalf("FetchResults = %d, want 12", updated.WebSearch.FetchResults)
	}
}

func TestHandleWebSearchSettings_MaxResultsOnlyUpdateAlignsFetchResults(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WebSearch.MaxResults = 5
	cfg.WebSearch.FetchResults = 12
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	handler := &Handler{configManager: cm}
	body, err := json.Marshal(map[string]interface{}{
		"max_results": 1,
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/web-search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleWebSearchSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := cm.Get()
	if updated.WebSearch.MaxResults != 1 {
		t.Fatalf("MaxResults = %d, want 1", updated.WebSearch.MaxResults)
	}
	if updated.WebSearch.FetchResults != 1 {
		t.Fatalf("FetchResults = %d, want 1", updated.WebSearch.FetchResults)
	}
}
