package api

import (
	"bytes"
	"cornerstone/client"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleProviderByID_SamplingToggles(t *testing.T) {
	provider := config.DefaultProvider()
	provider.ID = "provider-1"
	provider.Name = "Test"
	provider.APIKey = "secret-key"
	provider.Model = "gpt-test"

	handler := &Handler{configManager: newTestProviderConfigManager(t, provider)}

	put := func(payload map[string]interface{}) *config.Provider {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal request failed: %v", err)
		}
		req := httptest.NewRequest(http.MethodPut, "/management/providers/provider-1", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.handleProviderByID(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		updated := handler.configManager.GetProvider("provider-1")
		if updated == nil {
			t.Fatalf("provider not found after update")
		}
		return updated
	}

	updated := put(map[string]interface{}{
		"name":                "Test",
		"type":                "anthropic",
		"model":               "claude-test",
		"temperature":         0.3,
		"temperature_enabled": true,
		"top_p":               0.9,
		"top_p_enabled":       true,
	})
	if !updated.TemperatureEnabled || updated.Temperature != 0.3 {
		t.Fatalf("temperature = %v enabled=%v, want 0.3 enabled", updated.Temperature, updated.TemperatureEnabled)
	}
	if !updated.TopPEnabled || updated.TopP != 0.9 {
		t.Fatalf("top_p = %v enabled=%v, want 0.9 enabled", updated.TopP, updated.TopPEnabled)
	}

	// 省略开关字段时保留原值
	updated = put(map[string]interface{}{"name": "Test", "type": "anthropic", "model": "claude-test"})
	if !updated.TemperatureEnabled || !updated.TopPEnabled {
		t.Fatalf("toggles not preserved: %+v", updated)
	}

	updated = put(map[string]interface{}{
		"name":                "Test",
		"type":                "anthropic",
		"model":               "claude-test",
		"temperature_enabled": false,
		"top_p_enabled":       false,
	})
	if updated.TemperatureEnabled || updated.TopPEnabled {
		t.Fatalf("toggles not disabled: %+v", updated)
	}
	if updated.Temperature != 0.3 || updated.TopP != 0.9 {
		t.Fatalf("values should be kept when disabled: temperature=%v top_p=%v", updated.Temperature, updated.TopP)
	}
}

func TestBuildChatRequestForProvider_SamplingParams(t *testing.T) {
	provider := config.DefaultProvider()
	messages := []client.Message{{Role: "user", Content: "hi"}}

	req := buildChatRequestForProvider(&provider, messages, nil, false, 0, nil)
	if req.Temperature != nil || req.TopP != nil {
		t.Fatalf("default provider should omit sampling params, got temperature=%v top_p=%v", req.Temperature, req.TopP)
	}

	provider.TemperatureEnabled = true
	provider.Temperature = 0
	provider.TopPEnabled = true
	provider.TopP = 0.5
	req = buildChatRequestForProvider(&provider, messages, nil, false, 0, nil)
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.5 {
		t.Fatalf("top_p = %v, want 0.5", req.TopP)
	}

	override := 1.2
	provider.TemperatureEnabled = false
	req = buildChatRequestForProvider(&provider, messages, nil, false, 0, &override)
	if req.Temperature == nil || *req.Temperature != 1.2 {
		t.Fatalf("override temperature = %v, want 1.2", req.Temperature)
	}
}
