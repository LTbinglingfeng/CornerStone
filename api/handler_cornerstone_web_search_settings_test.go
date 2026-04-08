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

func TestHandleCornerstoneWebSearchSettings_CanClearSecretsWithExplicitEmptyStrings(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.CornerstoneWebSearch.ActiveProviderID = "tavily"
	cfg.CornerstoneWebSearch.Providers = map[string]config.WebSearchProvider{
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

	req := httptest.NewRequest(http.MethodPut, cornerstoneWebSearchSettingsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleCornerstoneWebSearchSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := cm.Get()
	providerCfg, ok := updated.CornerstoneWebSearch.Providers["tavily"]
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
	if updated.CornerstoneWebSearch.FetchResults != 12 {
		t.Fatalf("FetchResults = %d, want 12", updated.CornerstoneWebSearch.FetchResults)
	}
}

func TestHandleCornerstoneWebSearchSettings_MaxResultsOnlyUpdateAlignsFetchResults(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.CornerstoneWebSearch.MaxResults = 5
	cfg.CornerstoneWebSearch.FetchResults = 12
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

	req := httptest.NewRequest(http.MethodPut, cornerstoneWebSearchSettingsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleCornerstoneWebSearchSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := cm.Get()
	if updated.CornerstoneWebSearch.MaxResults != 1 {
		t.Fatalf("MaxResults = %d, want 1", updated.CornerstoneWebSearch.MaxResults)
	}
	if updated.CornerstoneWebSearch.FetchResults != 1 {
		t.Fatalf("FetchResults = %d, want 1", updated.CornerstoneWebSearch.FetchResults)
	}
}

func TestHandleCornerstoneWebSearchSettings_ZhipuSearchEngineUpdate(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.CornerstoneWebSearch.ActiveProviderID = "zhipu"
	cfg.CornerstoneWebSearch.Providers = map[string]config.WebSearchProvider{
		"zhipu": {
			APIKey:       "secret-key",
			SearchEngine: "search_std",
		},
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	handler := &Handler{configManager: cm}
	body, err := json.Marshal(map[string]interface{}{
		"providers": map[string]interface{}{
			"zhipu": map[string]string{
				"search_engine": "search_pro_quark",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, cornerstoneWebSearchSettingsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleCornerstoneWebSearchSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated := cm.Get()
	providerCfg, ok := updated.CornerstoneWebSearch.Providers["zhipu"]
	if !ok {
		t.Fatalf("provider settings missing after update")
	}
	if providerCfg.SearchEngine != "search_pro_quark" {
		t.Fatalf("SearchEngine = %q, want search_pro_quark", providerCfg.SearchEngine)
	}
}

func TestHandleCornerstoneWebSearchSettings_ZhipuSearchEngineRejectsInvalidValue(t *testing.T) {
	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.CornerstoneWebSearch.Providers = map[string]config.WebSearchProvider{
		"zhipu": {
			APIKey: "secret-key",
		},
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	handler := &Handler{configManager: cm}
	body, err := json.Marshal(map[string]interface{}{
		"providers": map[string]interface{}{
			"zhipu": map[string]string{
				"search_engine": "not_real",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, cornerstoneWebSearchSettingsPath, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleCornerstoneWebSearchSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
