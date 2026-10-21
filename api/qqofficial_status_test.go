package api

import (
	"cornerstone/config"
	"os"
	"path/filepath"
	"testing"
)

func TestQQOfficialMissingCredentialsStatusContract(t *testing.T) {
	// Loading an existing incomplete configuration must report the status used
	// by frontend types/translations, even though saving one is rejected.
	for _, raw := range []string{
		`{"qq_official":{"enabled":true,"app_id":"123"}}`,
		`{"qq_official":{"enabled":true,"app_secret":"test-only"}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			h := &Handler{configManager: config.NewManager(path)}
			s := NewQQOfficialService(h)
			t.Cleanup(s.Close)
			s.mu.Lock()
			defer s.mu.Unlock()
			if got := s.status.Status; got != "missing_credentials" {
				t.Fatalf("service status = %q", got)
			}
			if got := s.settingsLocked()["status"]; got != "missing_credentials" {
				t.Fatalf("API status = %q", got)
			}
		})
	}
}
