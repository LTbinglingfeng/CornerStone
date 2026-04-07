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
