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
