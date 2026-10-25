package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func officialSessionStore(t *testing.T) *QQOfficialSessionStore {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewQQOfficialSessionStore(filepath.Join(dir, "sessions.json"))
}
func assertOfficialSelection(t *testing.T, s *QQOfficialSessionStore, scope, want string) {
	t.Helper()
	got, err := s.Get(scope)
	if err != nil || got != want {
		t.Fatalf("Get(%q) = %q, %v; want %q", scope, got, err, want)
	}
}
func TestQQOfficialSessionsLifecycle(t *testing.T) {
	s := officialSessionStore(t)
	assertOfficialSelection(t, s, "scope", "")
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("read created file")
	}
	for scope, id := range map[string]string{"scope": "session-1", "other": "session-2"} {
		if err := s.Set(scope, id); err != nil {
			t.Fatal(err)
		}
	}
	s = NewQQOfficialSessionStore(s.path)
	assertOfficialSelection(t, s, "scope", "session-1")
	if err := s.Set("scope", "session-3"); err != nil {
		t.Fatal(err)
	}
	assertOfficialSelection(t, NewQQOfficialSessionStore(s.path), "scope", "session-3")
	if err := s.Set("scope", ""); err != nil {
		t.Fatal(err)
	}
	s = NewQQOfficialSessionStore(s.path)
	assertOfficialSelection(t, s, "scope", "")
	assertOfficialSelection(t, s, "other", "session-2")
}
func TestQQOfficialSessionsCorrupt(t *testing.T) {
	for i, data := range []string{"{", "null", "[]", `{"scope":null}`, `{"scope":""}`, `{"../bad":"session"}`, `{"scope":"../bad"}`, `{"scope":1}`, `{"scope":"one","scope":"two"}`, `{} {}`, strings.Repeat(" ", qqOfficialSessionMaxBytes+1)} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s := officialSessionStore(t)
			if err := os.WriteFile(s.path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if got, err := s.Get("scope"); err == nil || got != "" {
				t.Fatal("corrupt store accepted")
			}
			if err := s.Set("scope", "session"); err == nil {
				t.Fatal("corrupt store overwritten")
			}
			got, err := os.ReadFile(s.path)
			if err != nil || string(got) != data || s.loaded {
				t.Fatal("corrupt store changed or cached")
			}
		})
	}
}
func TestQQOfficialSessionsInvalidIDs(t *testing.T) {
	s := officialSessionStore(t)
	for _, id := range []string{"", "../bad", "a/b", "a b", strings.Repeat("a", 129)} {
		if _, err := s.Get(id); err == nil {
			t.Fatal("invalid scope accepted")
		}
		if err := s.Set(id, "session"); err == nil {
			t.Fatal("invalid scope written")
		}
		if id != "" {
			if err := s.Set("scope", id); err == nil {
				t.Fatal("invalid session written")
			}
		}
	}
	if s.loaded {
		t.Fatal("invalid inputs accessed storage")
	}
}
func TestQQOfficialSessionsSymlinks(t *testing.T) {
	for _, kind := range []string{"file", "dangling", "parent", "loaded"} {
		t.Run(kind, func(t *testing.T) {
			s := officialSessionStore(t)
			target := filepath.Join(filepath.Dir(s.path), "target")
			if kind == "parent" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			} else if kind != "dangling" {
				if err := os.WriteFile(target, []byte(`{"scope":"old"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "loaded" {
				assertOfficialSelection(t, s, "scope", "")
			}
			if err := os.Symlink(target, s.path); err != nil {
				t.Skip(err)
			}
			if kind == "parent" {
				s.path = filepath.Join(s.path, "sessions.json")
			}
			if _, err := s.Get("scope"); err == nil {
				t.Fatal("symlink read")
			}
			if err := s.Set("scope", "new"); err == nil {
				t.Fatal("symlink written")
			}
		})
	}
}
func TestQQOfficialSessionsSaveFailure(t *testing.T) {
	s := officialSessionStore(t)
	if err := s.Set("scope", "old"); err != nil {
		t.Fatal(err)
	}
	original := s.path
	// A non-directory parent reliably fails even when tests run as root.
	s.path = filepath.Join(original, "sessions.json")
	for _, id := range []string{"new", ""} {
		if err := s.Set("scope", id); err == nil {
			t.Fatal("save succeeded")
		}
		if s.sessions["scope"] != "old" {
			t.Fatal("failed save changed memory")
		}
	}
	s.path = original
	assertOfficialSelection(t, NewQQOfficialSessionStore(original), "scope", "old")
	if err := s.Set("scope", "new"); err != nil {
		t.Fatal(err)
	}
	assertOfficialSelection(t, NewQQOfficialSessionStore(original), "scope", "new")
}
func TestQQOfficialSessionsCapacity(t *testing.T) {
	s := officialSessionStore(t)
	s.loaded = true
	s.sessions = make(map[string]string)
	for i := 0; i < qqOfficialSessionLimit; i++ {
		s.sessions[fmt.Sprint(i)] = "session"
	}
	if err := s.Set("extra", "session"); err == nil {
		t.Fatal("capacity exceeded")
	}
	if len(s.sessions) != qqOfficialSessionLimit {
		t.Fatal("failed save mutated map")
	}
	if err := s.Set("0", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("extra", "session"); err != nil {
		t.Fatal(err)
	}
	s.sessions["overflow"] = "session"
	data, err := json.Marshal(s.sessions)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQQOfficialSessionStore(s.path).Get("0"); err == nil {
		t.Fatal("oversized map loaded")
	}
}
func TestQQOfficialSessionsConcurrent(t *testing.T) {
	s := officialSessionStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			scope := fmt.Sprint(i)
			if err := s.Set(scope, "session"); err != nil {
				t.Error(err)
			}
			assertOfficialSelection(t, s, scope, "session")
		}(i)
	}
	wg.Wait()
	s = NewQQOfficialSessionStore(s.path)
	for i := 0; i < 20; i++ {
		assertOfficialSelection(t, s, fmt.Sprint(i), "session")
	}
}

func TestQQOfficialSessionsWriteRollback(t *testing.T) {
	s := officialSessionStore(t)
	if err := s.Set("scope", "old"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(s.path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Skip(err)
	}
	defer os.Chmod(dir, 0700)
	probe, err := os.CreateTemp(dir, "probe")
	if err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("directory permissions not enforced")
	}
	for _, id := range []string{"new", ""} {
		if err := s.Set("scope", id); err == nil {
			t.Fatal("write unexpectedly succeeded")
		}
		assertOfficialSelection(t, s, "scope", "old")
	}
	after, err := os.ReadFile(s.path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed write changed disk")
	}
}
