package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQQOfficialConfigDefaultsValidationAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := NewManager(path)
	c := m.GetQQOfficialConfig()
	if c.Enabled || c.SourceFilterMode != "all" {
		t.Fatalf("bad defaults %#v", c)
	}
	c = QQOfficialConfig{Enabled: true, AppID: "123", AppSecret: "fake", AllowedUserOpenIDs: []string{" opaque-user ", "opaque-user", "000AbC", ""}}
	if err := m.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	got := m.GetQQOfficialConfig()
	if !reflect.DeepEqual(got.AllowedUserOpenIDs, []string{"opaque-user", "000AbC"}) {
		t.Fatal("openid was treated as numeric QQ id")
	}
	got.AllowedUserOpenIDs[0] = "mutated"
	if m.GetQQOfficialConfig().AllowedUserOpenIDs[0] != "opaque-user" {
		t.Fatal("shared snapshot")
	}
	loaded := NewManager(path)
	if loaded.GetQQOfficialConfig().AppSecret != "fake" {
		t.Fatal("credentials not persisted")
	}
	for _, bad := range []QQOfficialConfig{{Enabled: true}, {Enabled: true, AppID: "123"}, {AppID: "not-digit"}, {SourceFilterMode: "unknown"}} {
		if m.UpdateQQOfficialConfig(bad) == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
	before := m.GetQQOfficialConfig()
	os.Remove(path)
	os.Mkdir(path, 0700)
	os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0600)
	c.AppID = "456"
	if m.UpdateQQOfficialConfig(c) == nil {
		t.Fatal("expected save failure")
	}
	if !reflect.DeepEqual(before, m.GetQQOfficialConfig()) {
		t.Fatal("save failure did not rollback")
	}
}
