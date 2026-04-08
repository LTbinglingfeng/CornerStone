package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeTimeZone(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "Asia/Shanghai", want: "Asia/Shanghai"},
		{input: "  America/New_York  ", want: "America/New_York"},
		{input: "", want: DefaultTimeZone},
		{input: "Invalid/Zone", want: DefaultTimeZone},
	}

	for _, tc := range tests {
		if got := normalizeTimeZone(tc.input); got != tc.want {
			t.Fatalf("normalizeTimeZone(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestManagerLoadFallsBackToDefaultTimeZone(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	cfg := DefaultConfig()
	cfg.TimeZone = "Invalid/Zone"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("Write config failed: %v", err)
	}

	manager := NewManager(configPath)
	if got := manager.Get().TimeZone; got != DefaultTimeZone {
		t.Fatalf("TimeZone = %q, want %q", got, DefaultTimeZone)
	}
}

func TestDefaultConfig_IncludesIdleGreetingDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.IdleGreeting.Enabled {
		t.Fatal("IdleGreeting.Enabled = true, want false")
	}
	if cfg.IdleGreeting.IdleMinMinutes != DefaultIdleGreetingMinMinutes {
		t.Fatalf("IdleGreeting.IdleMinMinutes = %d, want %d", cfg.IdleGreeting.IdleMinMinutes, DefaultIdleGreetingMinMinutes)
	}
	if cfg.IdleGreeting.IdleMaxMinutes != DefaultIdleGreetingMaxMinutes {
		t.Fatalf("IdleGreeting.IdleMaxMinutes = %d, want %d", cfg.IdleGreeting.IdleMaxMinutes, DefaultIdleGreetingMaxMinutes)
	}
	if len(cfg.IdleGreeting.TimeWindows) != 1 {
		t.Fatalf("IdleGreeting.TimeWindows len = %d, want 1", len(cfg.IdleGreeting.TimeWindows))
	}
	if cfg.IdleGreeting.TimeWindows[0].Start != "09:00" || cfg.IdleGreeting.TimeWindows[0].End != "22:00" {
		t.Fatalf("IdleGreeting.TimeWindows[0] = %#v, want 09:00-22:00", cfg.IdleGreeting.TimeWindows[0])
	}
}

func TestManagerLoadNormalizesIdleGreetingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	cfg := DefaultConfig()
	cfg.IdleGreeting = IdleGreetingConfig{
		Enabled: true,
		TimeWindows: []IdleGreetingTimeWindow{
			{Start: "bad", End: "22:00"},
			{Start: "22:00", End: "02:00"},
			{Start: "22:00", End: "02:00"},
		},
		IdleMinMinutes: 180,
		IdleMaxMinutes: 60,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config failed: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("Write config failed: %v", err)
	}

	manager := NewManager(configPath)
	got := manager.Get().IdleGreeting
	if !got.Enabled {
		t.Fatal("IdleGreeting.Enabled = false, want true")
	}
	if got.IdleMinMinutes != 60 || got.IdleMaxMinutes != 180 {
		t.Fatalf("IdleGreeting minutes = %d-%d, want 60-180", got.IdleMinMinutes, got.IdleMaxMinutes)
	}
	if len(got.TimeWindows) != 1 {
		t.Fatalf("IdleGreeting.TimeWindows len = %d, want 1", len(got.TimeWindows))
	}
	if got.TimeWindows[0].Start != "22:00" || got.TimeWindows[0].End != "02:00" {
		t.Fatalf("IdleGreeting.TimeWindows[0] = %#v, want 22:00-02:00", got.TimeWindows[0])
	}
}

func TestManagerLoadMigratesLegacyWebSearchConfigKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")

	raw := []byte(`{
  "providers": [{"id":"default","name":"OpenAI","type":"openai","base_url":"https://api.openai.com/v1","api_key":"","model":"gpt-4o-mini"}],
  "active_provider_id": "default",
  "tool_toggles": {
    "get_time": true,
    "web_search": false
  },
  "web_search": {
    "active_provider_id": "tavily",
    "providers": {
      "tavily": {
        "api_key": "secret-key"
      }
    },
    "max_results": 3,
    "fetch_results": 4,
    "timeout_seconds": 9
  }
}`)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatalf("Write config failed: %v", err)
	}

	manager := NewManager(configPath)
	got := manager.Get()
	if got.CornerstoneWebSearch.ActiveProviderID != "tavily" {
		t.Fatalf("CornerstoneWebSearch.ActiveProviderID = %q, want tavily", got.CornerstoneWebSearch.ActiveProviderID)
	}
	if got.CornerstoneWebSearch.Providers["tavily"].APIKey != "secret-key" {
		t.Fatalf("CornerstoneWebSearch provider API key missing after migration: %#v", got.CornerstoneWebSearch.Providers)
	}
	if got.ToolToggles[CornerstoneWebSearchKey] {
		t.Fatalf("ToolToggles[%q] = true, want false", CornerstoneWebSearchKey)
	}
	if _, ok := got.ToolToggles[LegacyWebSearchKey]; ok {
		t.Fatalf("legacy toggle key %q unexpectedly preserved", LegacyWebSearchKey)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read migrated config failed: %v", err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("Unmarshal migrated config failed: %v", err)
	}
	if _, ok := persisted[LegacyWebSearchKey]; ok {
		t.Fatalf("migrated config still contains legacy key: %s", string(data))
	}
	if _, ok := persisted[CornerstoneWebSearchKey]; !ok {
		t.Fatalf("migrated config missing canonical key: %s", string(data))
	}
}
