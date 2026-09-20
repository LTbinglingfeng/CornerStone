package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClawBotReferenceIsolationRestartExpiry(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a := ClawBotAccountKey("https://a", "token")
	conv := ClawBotReferenceConversation("peer", "session")
	raw := json.RawMessage(`[{"type":2,"future":{"number":18446744073709551615}}]`)
	if err := m.SaveClawBotReference(a, conv, "18446744073709551615", raw); err != nil {
		t.Fatal(err)
	}
	m = NewClawBotStateManager(dir)
	got, err := m.FindClawBotReference(a, conv, "18446744073709551615")
	if err != nil || string(got) != string(raw) {
		t.Fatalf("%s %v", got, err)
	}
	for _, key := range [][2]string{{strings.Repeat("b", 64), conv}, {a, ClawBotReferenceConversation("peer", "other")}, {a, ClawBotReferenceConversation("other", "session")}, {a, ClawBotReferenceConversation("peer", "")}} {
		got, err = m.FindClawBotReference(key[0], key[1], "18446744073709551615")
		if err != nil || got != nil {
			t.Fatal("cross-scope cache hit", err)
		}
	}
	got, err = m.clawBotReference(a, conv, "18446744073709551615", nil, time.Now().Add(clawBotReferenceTTL+time.Hour))
	if err != nil || got != nil {
		t.Fatal("expired", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "references", "items.json"))
	if string(b) != "[]" {
		t.Fatal(string(b))
	}
}

func TestClawBotReferenceEvictionAndCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	cacheDir := filepath.Join(dir, "references")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	entries := make([]clawBotReferenceEntry, clawBotReferenceCapacity+1)
	for i := range entries {
		entries[i] = clawBotReferenceEntry{Key: clawBotReferenceKey(strings.Repeat("a", 64), "c", fmt.Sprint(i)), At: now.Add(time.Duration(i-len(entries)) * time.Second), Items: json.RawMessage(`[]`)}
	}
	b, _ := json.Marshal(entries)
	path := filepath.Join(cacheDir, "items.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, ".items-crash"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveClawBotReference(strings.Repeat("a", 64), "c", "new", json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.FindClawBotReference(strings.Repeat("a", 64), "c", "0"); got != nil {
		t.Fatal("oldest not evicted")
	}
	if got, _ := m.FindClawBotReference(strings.Repeat("a", 64), "c", "new"); got == nil {
		t.Fatal("newest missing")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, ".items-crash")); !os.IsNotExist(err) {
		t.Fatal("crash temporary not cleaned")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	// A truncated/corrupt cache must not poison delivery state or future writes.
	if err := os.WriteFile(path, []byte(`[{"key":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveClawBotReference(strings.Repeat("a", 64), "c", "recovered", json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	if m.failed != nil {
		t.Fatal("cache poisoned durable state")
	}
	if got, _ := m.FindClawBotReference(strings.Repeat("a", 64), "c", "recovered"); got == nil {
		t.Fatal("recovery failed")
	}
}

func TestClawBotReferenceConcurrentSnapshotsAndFailure(t *testing.T) {
	m := NewClawBotStateManager(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			if err := m.SaveClawBotReference(strings.Repeat("a", 64), "c", id, json.RawMessage(`[{"type":1}]`)); err != nil {
				t.Error(err)
			}
			got, err := m.FindClawBotReference(strings.Repeat("a", 64), "c", id)
			if err != nil || got == nil {
				t.Error(err)
			}
			if len(got) > 0 {
				got[0] = 'X'
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 12; i++ {
		got, err := m.FindClawBotReference(strings.Repeat("a", 64), "c", fmt.Sprint(i))
		if err != nil || !json.Valid(got) {
			t.Fatal("snapshot mutated", err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "references"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	blocked := NewClawBotStateManager(dir)
	if err := blocked.SaveClawBotReference(strings.Repeat("a", 64), "c", "1", json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected IO error")
	}
	if blocked.failed != nil {
		t.Fatal("poisoned delivery manager")
	}
}

func TestClawBotReferenceByteBudget(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	cacheDir := filepath.Join(dir, "references")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(strings.Repeat("x", 250000))
	entries := make([]clawBotReferenceEntry, 66)
	now := time.Now()
	for i := range entries {
		entries[i] = clawBotReferenceEntry{Key: clawBotReferenceKey(strings.Repeat("a", 64), "c", fmt.Sprint(i)), At: now.Add(time.Duration(i-100) * time.Second), Items: raw}
	}
	data, _ := json.Marshal(entries)
	path := filepath.Join(cacheDir, "items.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := m.SaveClawBotReference(strings.Repeat("a", 64), "c", fmt.Sprint("new", i), raw); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > clawBotReferenceBytes {
		t.Fatal(info, err)
	}
	if got, _ := m.FindClawBotReference(strings.Repeat("a", 64), "c", "0"); got != nil {
		t.Fatal("byte limit did not evict oldest")
	}
	oversized, _ := json.Marshal(strings.Repeat("x", clawBotReferenceItemBytes))
	if err := m.SaveClawBotReference(strings.Repeat("a", 64), "c", "oversized", oversized); err == nil {
		t.Fatal("oversized accepted")
	}
}

func TestClawBotReferenceMigrationAndLocalMetadata(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b := ClawBotAccountKey("a", "token"), ClawBotAccountKey("a", "bot-id:1")
	raw := json.RawMessage(`[{"type":2}]`)
	if err := m.Save(a, ClawBotState{}); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveClawBotReference(a, "c", "id", raw); err != nil {
		t.Fatal(err)
	}
	local := []ClawBotReferenceMedia{{ItemIndex: 0, Attachment: ChatAttachment{Path: "cache_photo/test.png", Kind: "image"}}}
	if err := m.SaveClawBotReferenceMedia(a, "c", "id", local); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(a, b); err != nil {
		t.Fatal(err)
	}
	m = NewClawBotStateManager(dir)
	if got, err := m.FindClawBotReference(b, "c", "id"); err != nil || string(got) != string(raw) {
		t.Fatal(string(got), err)
	}
	if got, err := m.FindClawBotReferenceMedia(b, "c", "id"); err != nil || len(got) != 1 || got[0].Attachment.Path != local[0].Attachment.Path {
		t.Fatal(got, err)
	}
	if _, err := m.FindClawBotReference(a, "c", "id"); err == nil {
		t.Fatal("retired read")
	}
	if err := m.SaveClawBotReferenceMedia(a, "c", "id", local); err == nil {
		t.Fatal("retired metadata write")
	}
	if err := m.SaveClawBotReference(a, "c", "id", raw); err == nil {
		t.Fatal("retired write")
	}
	if _, err := m.FindClawBotReference("a", "c", "id"); err == nil {
		t.Fatal("invalid account accepted")
	}
	if _, err := m.clawBotReference(b, "c", "id", nil, time.Now().Add(clawBotReferenceTTL+time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.FindClawBotReferenceMedia(b, "c", "id"); len(got) != 0 {
		t.Fatal("expired metadata retained")
	}
}
