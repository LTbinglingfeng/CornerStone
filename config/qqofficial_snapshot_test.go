package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

// Reproduce the exact interleaving of a general settings GET, a binding save,
// and the delayed general settings UPDATE without scheduler-dependent sleeps.
func TestQQOfficialSurvivesStaleGeneralSettingsUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := NewManager(path)
	old := QQOfficialConfig{Enabled: true, AppID: "123", AppSecret: "old-test-secret", SourceFilterMode: "all"}
	if err := m.UpdateQQOfficialConfig(old); err != nil {
		t.Fatal(err)
	}
	stale := m.Get()
	next := QQOfficialConfig{Enabled: true, AppID: "456", AppSecret: "new-test-secret", PromptID: "persona-b", SourceFilterMode: "allowlist", AllowedUserOpenIDs: []string{"allowed-user"}}
	if err := m.UpdateQQOfficialConfig(next); err != nil {
		t.Fatal(err)
	}
	want := m.GetQQOfficialConfig()
	stale.MemoryEnabled = !stale.MemoryEnabled
	if err := m.Update(stale); err != nil {
		t.Fatal(err)
	}
	if got := m.GetQQOfficialConfig(); !reflect.DeepEqual(got, want) {
		t.Fatal("general settings reverted QQ credentials or access rules")
	}
	if m.Get().MemoryEnabled != stale.MemoryEnabled {
		t.Fatal("general settings were not saved")
	}
	loaded := NewManager(path)
	if !reflect.DeepEqual(loaded.GetQQOfficialConfig(), want) {
		t.Fatal("persisted QQ configuration was reverted")
	}
	// Revocation must be just as durable as a new binding.
	stale = m.Get()
	next.Enabled = false
	next.AllowedUserOpenIDs = nil
	if err := m.UpdateQQOfficialConfig(next); err != nil {
		t.Fatal(err)
	}
	want = m.GetQQOfficialConfig()
	if err := m.Update(stale); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.GetQQOfficialConfig(), want) {
		t.Fatal("stale snapshot undid revocation")
	}
	if !reflect.DeepEqual(NewManager(path).GetQQOfficialConfig(), want) {
		t.Fatal("persisted revocation was reverted")
	}
}
