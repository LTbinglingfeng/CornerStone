package api

import (
	"bytes"
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleConfig_TimeZoneRoundTripAndProvidersExposeIt(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	body, err := json.Marshal(map[string]interface{}{
		"time_zone": "  America/New_York  ",
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := handler.configManager.Get().TimeZone; got != "America/New_York" {
		t.Fatalf("saved time_zone = %q, want %q", got, "America/New_York")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/management/config", nil)
	getRec := httptest.NewRecorder()
	handler.handleConfig(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getRec.Code, http.StatusOK)
	}

	var configResp struct {
		Success bool `json:"success"`
		Data    struct {
			TimeZone string `json:"time_zone"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&configResp); err != nil {
		t.Fatalf("Decode config response failed: %v", err)
	}
	if !configResp.Success {
		t.Fatalf("config response success = false, body=%s", getRec.Body.String())
	}
	if configResp.Data.TimeZone != "America/New_York" {
		t.Fatalf("GET time_zone = %q, want %q", configResp.Data.TimeZone, "America/New_York")
	}

	providersReq := httptest.NewRequest(http.MethodGet, "/management/providers", nil)
	providersRec := httptest.NewRecorder()
	handler.handleProviders(providersRec, providersReq)

	if providersRec.Code != http.StatusOK {
		t.Fatalf("providers status = %d, want %d", providersRec.Code, http.StatusOK)
	}

	var providersResp struct {
		Success bool `json:"success"`
		Data    struct {
			TimeZone string `json:"time_zone"`
		} `json:"data"`
	}
	if err := json.NewDecoder(providersRec.Body).Decode(&providersResp); err != nil {
		t.Fatalf("Decode providers response failed: %v", err)
	}
	if !providersResp.Success {
		t.Fatalf("providers response success = false, body=%s", providersRec.Body.String())
	}
	if providersResp.Data.TimeZone != "America/New_York" {
		t.Fatalf("providers time_zone = %q, want %q", providersResp.Data.TimeZone, "America/New_York")
	}
}

func TestHandleConfig_TimeZoneRejectsInvalidValue(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	body, err := json.Marshal(map[string]interface{}{
		"time_zone": "Invalid/Zone",
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleConfig_ToolTogglesRoundTripAndProvidersExposeThem(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	body, err := json.Marshal(map[string]interface{}{
		"tool_toggles": map[string]bool{
			"get_time":              false,
			legacyWebSearchToolName: false,
			"red_packet_received":   false,
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", rec.Code, http.StatusOK)
	}

	saved := handler.configManager.Get().ToolToggles
	if saved["get_time"] {
		t.Fatal("saved get_time toggle = true, want false")
	}
	if saved[cornerstoneWebSearchToolName] {
		t.Fatalf("saved %s toggle = true, want false", cornerstoneWebSearchToolName)
	}
	if _, ok := saved[legacyWebSearchToolName]; ok {
		t.Fatalf("saved legacy toggle %q unexpectedly preserved", legacyWebSearchToolName)
	}
	if !saved["send_pat"] {
		t.Fatal("saved send_pat toggle = false, want true by default")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/management/config", nil)
	getRec := httptest.NewRecorder()
	handler.handleConfig(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getRec.Code, http.StatusOK)
	}

	var configResp struct {
		Success bool `json:"success"`
		Data    struct {
			ToolToggles map[string]bool `json:"tool_toggles"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&configResp); err != nil {
		t.Fatalf("Decode config response failed: %v", err)
	}
	if !configResp.Success {
		t.Fatalf("config response success = false, body=%s", getRec.Body.String())
	}
	if configResp.Data.ToolToggles["get_time"] {
		t.Fatal("GET get_time toggle = true, want false")
	}
	if configResp.Data.ToolToggles[cornerstoneWebSearchToolName] {
		t.Fatalf("GET %s toggle = true, want false", cornerstoneWebSearchToolName)
	}
	if !configResp.Data.ToolToggles["send_red_packet"] {
		t.Fatal("GET send_red_packet toggle = false, want true")
	}

	providersReq := httptest.NewRequest(http.MethodGet, "/management/providers", nil)
	providersRec := httptest.NewRecorder()
	handler.handleProviders(providersRec, providersReq)

	if providersRec.Code != http.StatusOK {
		t.Fatalf("providers status = %d, want %d", providersRec.Code, http.StatusOK)
	}

	var providersResp struct {
		Success bool `json:"success"`
		Data    struct {
			ToolToggles map[string]bool `json:"tool_toggles"`
		} `json:"data"`
	}
	if err := json.NewDecoder(providersRec.Body).Decode(&providersResp); err != nil {
		t.Fatalf("Decode providers response failed: %v", err)
	}
	if !providersResp.Success {
		t.Fatalf("providers response success = false, body=%s", providersRec.Body.String())
	}
	if providersResp.Data.ToolToggles[cornerstoneWebSearchToolName] {
		t.Fatalf("providers %s toggle = true, want false", cornerstoneWebSearchToolName)
	}
	if !providersResp.Data.ToolToggles["get_weather"] {
		t.Fatal("providers get_weather toggle = false, want true")
	}
}

func TestHandleConfig_IdleGreetingRoundTripAndProvidersExposeIt(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	body, err := json.Marshal(map[string]interface{}{
		"idle_greeting": map[string]interface{}{
			"enabled": true,
			"time_windows": []map[string]string{
				{"start": "08:30", "end": "12:00"},
				{"start": "22:00", "end": "01:30"},
			},
			"idle_min_minutes": 90,
			"idle_max_minutes": 150,
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", rec.Code, http.StatusOK)
	}

	saved := handler.configManager.Get().IdleGreeting
	if !saved.Enabled {
		t.Fatal("saved idle_greeting.enabled = false, want true")
	}
	if saved.IdleMinMinutes != 90 || saved.IdleMaxMinutes != 150 {
		t.Fatalf("saved idle_greeting minutes = %d-%d, want 90-150", saved.IdleMinMinutes, saved.IdleMaxMinutes)
	}
	if len(saved.TimeWindows) != 2 {
		t.Fatalf("saved time_windows len = %d, want 2", len(saved.TimeWindows))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/management/config", nil)
	getRec := httptest.NewRecorder()
	handler.handleConfig(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getRec.Code, http.StatusOK)
	}

	var configResp struct {
		Success bool `json:"success"`
		Data    struct {
			IdleGreeting config.IdleGreetingConfig `json:"idle_greeting"`
		} `json:"data"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&configResp); err != nil {
		t.Fatalf("Decode config response failed: %v", err)
	}
	if !configResp.Success {
		t.Fatalf("config response success = false, body=%s", getRec.Body.String())
	}
	if !configResp.Data.IdleGreeting.Enabled {
		t.Fatal("GET idle_greeting.enabled = false, want true")
	}
	if configResp.Data.IdleGreeting.IdleMinMinutes != 90 || configResp.Data.IdleGreeting.IdleMaxMinutes != 150 {
		t.Fatalf(
			"GET idle_greeting minutes = %d-%d, want 90-150",
			configResp.Data.IdleGreeting.IdleMinMinutes,
			configResp.Data.IdleGreeting.IdleMaxMinutes,
		)
	}

	providersReq := httptest.NewRequest(http.MethodGet, "/management/providers", nil)
	providersRec := httptest.NewRecorder()
	handler.handleProviders(providersRec, providersReq)

	if providersRec.Code != http.StatusOK {
		t.Fatalf("providers status = %d, want %d", providersRec.Code, http.StatusOK)
	}

	var providersResp struct {
		Success bool `json:"success"`
		Data    struct {
			IdleGreeting config.IdleGreetingConfig `json:"idle_greeting"`
		} `json:"data"`
	}
	if err := json.NewDecoder(providersRec.Body).Decode(&providersResp); err != nil {
		t.Fatalf("Decode providers response failed: %v", err)
	}
	if !providersResp.Success {
		t.Fatalf("providers response success = false, body=%s", providersRec.Body.String())
	}
	if len(providersResp.Data.IdleGreeting.TimeWindows) != 2 {
		t.Fatalf("providers idle_greeting.time_windows len = %d, want 2", len(providersResp.Data.IdleGreeting.TimeWindows))
	}
}

func TestHandleConfig_IdleGreetingRejectsInvalidValue(t *testing.T) {
	handler := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("provider-1"))}

	body, err := json.Marshal(map[string]interface{}{
		"idle_greeting": map[string]interface{}{
			"enabled": true,
			"time_windows": []map[string]string{
				{"start": "09:00", "end": "09:00"},
			},
			"idle_min_minutes": 200,
			"idle_max_minutes": 100,
		},
	})
	if err != nil {
		t.Fatalf("Marshal request failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/management/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.handleConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
