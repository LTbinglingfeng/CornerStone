package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestClawBotAccountKey(t *testing.T) {
	key := ClawBotAccountKey(" https://EXAMPLE.com/api/// ", "secret")
	if len(key) != 64 || key != ClawBotAccountKey("https://example.com/api", "secret") {
		t.Fatal("expected normalized hex identity")
	}
	for _, pair := range [][2]string{
		{"https://example.com/api", "other"},
		{"https://other.com/api", "secret"},
		{"https://example.com/API", "secret"},
		{"https://example.com/api", " secret"},
	} {
		if key == ClawBotAccountKey(pair[0], pair[1]) {
			t.Fatal("distinct accounts share a key")
		}
	}
	if ClawBotAccountKey("ab", "c") == ClawBotAccountKey("a", "bc") {
		t.Fatal("identity framing is ambiguous")
	}
}

func TestClawBotStateRecoveryIsolationAndSnapshots(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "states")
	m := NewClawBotStateManager(dir)
	key := ClawBotAccountKey("https://example.com", "raw-secret-token")
	other := ClawBotAccountKey("https://example.com", "other-token")
	if state, err := m.Load(key); err != nil || !reflect.DeepEqual(state, ClawBotState{}) {
		t.Fatalf("missing account: %+v, %v", state, err)
	}
	want := ClawBotState{GetUpdatesBuf: "cursor", ContextTokens: map[string]string{"user": "context"}, RecentMessageIDs: []string{"message"}}
	input := ClawBotState{GetUpdatesBuf: "cursor", ContextTokens: map[string]string{"user": "context"}, RecentMessageIDs: []string{"message"}}
	if err := m.Save(key, input); err != nil {
		t.Fatal(err)
	}
	input.ContextTokens["user"] = "changed"
	input.RecentMessageIDs[0] = "changed"
	if err := m.Save(other, ClawBotState{GetUpdatesBuf: "other"}); err != nil {
		t.Fatal(err)
	}
	for _, manager := range []*ClawBotStateManager{m, NewClawBotStateManager(dir)} {
		got, err := manager.Load(key)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("snapshot: %+v, %v", got, err)
		}
		got.ContextTokens["user"] = "mutated"
		got.RecentMessageIDs[0] = "mutated"
		got, err = manager.Load(key)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("load leaked references: %+v, %v", got, err)
		}
		got, err = manager.Load(other)
		if err != nil || got.GetUpdatesBuf != "other" {
			t.Fatalf("account isolation: %+v, %v", got, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil || bytes.Contains(data, []byte("raw-secret-token")) {
		t.Fatalf("credential leaked or read failed: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected only two account files: %v", err)
	}
}

func TestClawBotStateInvalidKeys(t *testing.T) {
	m := NewClawBotStateManager(t.TempDir())
	for _, key := range []string{"", "../escape", strings.Repeat("a", 63), strings.Repeat("g", 64), strings.Repeat("a", 62) + "/.", strings.Repeat("a", 65)} {
		if _, err := m.Load(key); err == nil {
			t.Fatalf("Load accepted %q", key)
		}
		if err := m.Save(key, ClawBotState{}); err == nil {
			t.Fatalf("Save accepted %q", key)
		}
	}
}

func TestClawBotStatePermissionsAndReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := filepath.Join(t.TempDir(), "states")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	m := NewClawBotStateManager(dir)
	key := ClawBotAccountKey("url", "token")
	for _, cursor := range []string{"first", "second"} {
		if err := m.Save(key, ClawBotState{GetUpdatesBuf: cursor}); err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, key+".json"): 0600} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Fatalf("%s: mode %o, want %o", path, info.Mode().Perm(), want)
			}
		}
		got, err := m.Load(key)
		if err != nil || got.GetUpdatesBuf != cursor {
			t.Fatalf("replacement: %+v, %v", got, err)
		}
	}
}

func TestClawBotStateCorruptionAndLimits(t *testing.T) {
	for _, data := range []string{"{", "null", "[]", "{} {}", `{"context_tokens":42}`, strings.Repeat(" ", maxClawBotStateBytes+1)} {
		dir := t.TempDir()
		m := NewClawBotStateManager(dir)
		key := ClawBotAccountKey("url", "token")
		path := filepath.Join(dir, key+".json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Load(key); err == nil {
			t.Fatal("Load accepted corrupt/oversized data")
		}
		if err := m.Save(key, ClawBotState{}); err == nil {
			t.Fatal("Save overwrote corrupt/oversized data")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatal("corrupt file was altered")
		}
	}
	m := NewClawBotStateManager(t.TempDir())
	key := ClawBotAccountKey("url", "token")
	if err := m.Save(key, ClawBotState{GetUpdatesBuf: strings.Repeat("x", maxClawBotStateBytes)}); err == nil {
		t.Fatal("Save accepted oversized state")
	}
	if got, err := m.Load(key); err != nil || got.GetUpdatesBuf != "" {
		t.Fatalf("failed Save left data: %+v, %v", got, err)
	}
}

func TestClawBotStateConcurrentAccess(t *testing.T) {
	m := NewClawBotStateManager(t.TempDir())
	key := ClawBotAccountKey("url", "token")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := ClawBotState{GetUpdatesBuf: "cursor", ContextTokens: map[string]string{"user": "context"}, RecentMessageIDs: []string{"message"}}
			if err := m.Save(key, state); err != nil {
				t.Error(err)
			}
			got, err := m.Load(key)
			if err != nil || !reflect.DeepEqual(got, state) {
				t.Errorf("concurrent snapshot: %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
}
