package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func receiptID(n int) string { return fmt.Sprintf("qqofficial-in-%064x", n) }
func receiptStore(t *testing.T) (*QQOfficialReceiptStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dir, err := filepath.EvalSymlinks(t.TempDir()) // macOS /var is a system symlink.
	if err != nil {
		t.Fatal(err)
	}
	s := NewQQOfficialReceiptStore(filepath.Join(dir, "receipts.json"))
	s.now = func() time.Time { return now }
	return s, &now
}
func assertSeen(t *testing.T, s *QQOfficialReceiptStore, id string, want bool) {
	t.Helper()
	got, err := s.Seen(id)
	if err != nil || got != want {
		t.Fatalf("Seen = %v, %v; want %v", got, err, want)
	}
}
func TestQQOfficialReceiptsLifecycle(t *testing.T) {
	s, now := receiptStore(t)
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("constructor must not create file")
	}
	assertSeen(t, s, receiptID(1), false)
	if err := s.Record(receiptID(1)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.path)
	*now = now.Add(time.Hour)
	if err := s.Record(receiptID(1)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) {
		t.Fatal("idempotent Record changed snapshot or timestamp")
	}
	history := filepath.Join(filepath.Dir(s.path), "chat.json")
	if err := os.WriteFile(history, []byte("chat"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(history); err != nil {
		t.Fatal(err)
	}
	reloaded := NewQQOfficialReceiptStore(s.path)
	reloaded.now = s.now
	assertSeen(t, reloaded, receiptID(1), true)
	*now = now.Add(qqOfficialReceiptTTL - time.Hour)
	assertSeen(t, s, receiptID(1), false)
	after, _ = os.ReadFile(s.path)
	if string(before) != string(after) {
		t.Fatal("Seen wrote file")
	}
	if err := s.Record(receiptID(2)); err != nil {
		t.Fatal(err)
	}
	if len(s.receipts) != 1 {
		t.Fatal("expired entry not pruned")
	}
	assertSeen(t, s, receiptID(2), true)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("receipt file must be 0600")
		}
	}
}
func TestQQOfficialReceiptsInvalidFiles(t *testing.T) {
	timestamp := `"2026-01-01T00:00:00Z"`
	cases := []string{
		`{`, `null`, `[]`, `{}`, `{"version":2,"receipts":{}}`,
		`{"version":1,"receipts":null}`, `{"version":1,"receipts":{},"secret":"PRIVATE"}`,
		`{"version":1,"version":1,"receipts":{}}`, `{"version":1,"receipts":{}} {}`,
		`{"version":1,"receipts":{"PRIVATE":` + timestamp + `}}`,
		`{"version":1,"receipts":{"` + receiptID(1) + `":"PRIVATE"}}`,
		`{"version":1,"receipts":{"` + receiptID(1) + `":null}}`,
		`{"version":1,"receipts":{"` + receiptID(1) + `":` + timestamp + `,"` + receiptID(1) + `":` + timestamp + `}}`,
		strings.Repeat(" ", qqOfficialReceiptMaxBytes+1),
	}
	for i, data := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s, _ := receiptStore(t)
			if err := os.WriteFile(s.path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if seen, err := s.Seen(receiptID(1)); err == nil || seen || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("not fail closed: %v %v", seen, err)
			}
			if err := s.Record(receiptID(1)); err == nil {
				t.Fatal("Record accepted corrupt snapshot")
			}
			got, _ := os.ReadFile(s.path)
			if string(got) != data || s.loaded {
				t.Fatal("corrupt snapshot changed or cached as empty")
			}
		})
	}
}
func TestQQOfficialReceiptsInvalidIDs(t *testing.T) {
	s, _ := receiptStore(t)
	for _, id := range []string{"", "PRIVATE", receiptID(1) + "0", strings.ToUpper(receiptID(1)), "qqofficial-in-" + strings.Repeat("g", 64), receiptID(1)[:77]} {
		if _, err := s.Seen(id); err == nil {
			t.Fatal("invalid ID accepted")
		}
		if err := s.Record(id); err == nil {
			t.Fatal("invalid ID recorded")
		}
	}
	if s.loaded {
		t.Fatal("invalid IDs touched storage")
	}
}
func TestQQOfficialReceiptsSymlinks(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			s, _ := receiptStore(t)
			target := filepath.Join(t.TempDir(), "missing.json")
			if parent {
				target = t.TempDir()
			}
			if err := os.Symlink(target, s.path); err != nil {
				t.Skip(err)
			}
			if parent {
				s.path = filepath.Join(s.path, "receipts.json")
			}
			if _, err := s.Seen(receiptID(1)); err == nil {
				t.Fatal("symlink accepted")
			}
			if err := s.Record(receiptID(1)); err == nil {
				t.Fatal("symlink written")
			}
		})
	}
}
func TestQQOfficialReceiptsCapacity(t *testing.T) {
	s, now := receiptStore(t)
	s.loaded = true
	s.receipts = make(map[string]time.Time)
	for i := 0; i < qqOfficialReceiptLimit; i++ {
		s.receipts[receiptID(i)] = *now
	}
	if err := s.Record(receiptID(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(receiptID(qqOfficialReceiptLimit)); err == nil {
		t.Fatal("capacity exceeded")
	}
	if len(s.receipts) != qqOfficialReceiptLimit {
		t.Fatal("live entries evicted")
	}
	data, err := json.Marshal(struct {
		Version  int                  `json:"version"`
		Receipts map[string]time.Time `json:"receipts"`
	}{1, s.receipts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reload := NewQQOfficialReceiptStore(s.path)
	reload.now = s.now
	assertSeen(t, reload, receiptID(0), true)
	s.receipts[receiptID(qqOfficialReceiptLimit)] = *now
	data, _ = json.Marshal(struct {
		Version  int                  `json:"version"`
		Receipts map[string]time.Time `json:"receipts"`
	}{1, s.receipts})
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reload = NewQQOfficialReceiptStore(s.path)
	reload.now = s.now
	if _, err := reload.Seen(receiptID(0)); err == nil {
		t.Fatal("over-capacity snapshot accepted")
	}
	*now = now.Add(qqOfficialReceiptTTL)
	if err := reload.Record(receiptID(qqOfficialReceiptLimit + 1)); err != nil {
		t.Fatal(err)
	}
	if len(reload.receipts) != 1 {
		t.Fatal("expired entries consume capacity")
	}
}
func TestQQOfficialReceiptsSaveRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	s, now := receiptStore(t)
	if err := s.Record(receiptID(1)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.path)
	*now = now.Add(qqOfficialReceiptTTL)
	dir := filepath.Dir(s.path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	probe, err := os.CreateTemp(dir, "probe")
	if err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("privileged user bypasses directory permissions")
	}
	if err := s.Record(receiptID(2)); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if len(s.receipts) != 1 || s.receipts[receiptID(1)].IsZero() {
		t.Fatal("failed save pruned original map")
	}
	assertSeen(t, s, receiptID(2), false)
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) {
		t.Fatal("failed save changed disk")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(receiptID(2)); err != nil {
		t.Fatal(err)
	}
	assertSeen(t, s, receiptID(2), true)
}
func TestQQOfficialReceiptsConcurrent(t *testing.T) {
	s, _ := receiptStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Record(receiptID(i)); err != nil {
				t.Error(err)
			}
			assertSeen(t, s, receiptID(i), true)
		}(i)
	}
	wg.Wait()
	reload := NewQQOfficialReceiptStore(s.path)
	reload.now = s.now
	for i := 0; i < 20; i++ {
		assertSeen(t, reload, receiptID(i), true)
	}
}
