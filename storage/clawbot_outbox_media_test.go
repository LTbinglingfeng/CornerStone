package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run on every platform, including Windows: a new blob must succeed on the
// first save, not only on a retry that bypasses the directory-sync path.
func TestClawBotDeliveryBlobFirstSaveAndRestart(t *testing.T) {
	dir := t.TempDir()
	account := strings.Repeat("a", 64)
	m := NewClawBotStateManager(dir)
	for _, content := range []string{"first attachment", "second attachment"} {
		blob, err := m.SaveDeliveryMedia(account, []byte(content))
		if err != nil {
			t.Fatalf("first save of %q failed: %v", content, err)
		}
		restarted := NewClawBotStateManager(dir)
		data, err := restarted.ReadDeliveryMedia(account, blob)
		if err != nil || string(data) != content {
			t.Fatalf("read after restart = %q, %v; want %q", data, err, content)
		}
		reused, err := restarted.SaveDeliveryMedia(account, []byte(content))
		if err != nil || reused != blob {
			t.Fatalf("repeat save = %q, %v; want %q", reused, err, blob)
		}
	}
}

func TestClawBotDeliveryBlobIsolationMigrationAndIntegrity(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	if err := m.Save(a, ClawBotState{}); err != nil {
		t.Fatal(err)
	}
	blob, err := m.SaveDeliveryMedia(a, []byte("immutable content"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ReadDeliveryMedia(b, blob); err == nil {
		t.Fatal("cross-account read")
	}
	if err = m.Migrate(a, b); err != nil {
		t.Fatal(err)
	}
	restarted := NewClawBotStateManager(dir)
	data, err := restarted.ReadDeliveryMedia(b, blob)
	if err != nil || string(data) != "immutable content" {
		t.Fatalf("migration lost blob: %s %v", data, err)
	}
	if _, err = restarted.ReadDeliveryMedia(a, blob); err == nil {
		t.Fatal("retired account can access new account blob")
	}
	if err = restarted.Migrate(b, c); err != nil {
		t.Fatal(err)
	}
	data, err = restarted.ReadDeliveryMedia(c, blob)
	if err != nil || string(data) != "immutable content" {
		t.Fatal(err)
	}
	mediaDir, err := restarted.mediaDir(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(mediaDir, blob), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.ReadDeliveryMedia(c, blob); err == nil {
		t.Fatal("corruption undetected")
	}
	if _, err = restarted.ReadDeliveryMedia(c, "../secret"); err == nil {
		t.Fatal("unsafe path")
	}
}

func TestClawBotDeliveryBlobCleanupProtectsAuditAndQuota(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	account := strings.Repeat("a", 64)
	live, err := m.SaveDeliveryMedia(account, []byte("audit"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := m.SaveDeliveryMedia(account, []byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := m.SaveDeliveryMedia(account, []byte("staged"))
	if err != nil {
		t.Fatal(err)
	}
	state := ClawBotState{Delivery: &ClawBotDeliveryState{Jobs: []ClawBotDeliveryJob{{Status: "cancelled", CancelRequested: true, Chunks: []ClawBotDeliveryChunk{{Media: &ClawBotDeliveryMedia{BlobID: live}}}}}}}
	if err = m.Save(account, state); err != nil {
		t.Fatal(err)
	}
	mediaDir, err := m.mediaDir(account)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []string{live, orphan} {
		if err = os.Chtimes(filepath.Join(mediaDir, id), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.CleanupDeliveryMedia(account, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ReadDeliveryMedia(account, live); err != nil {
		t.Fatal("deleted cancellation audit blob")
	}
	if _, err = m.ReadDeliveryMedia(account, fresh); err != nil {
		t.Fatal("deleted freshly staged blob")
	}
	if _, err = m.ReadDeliveryMedia(account, orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan retained: %v", err)
	}
	// Sparse file exercises quota without allocating a gigabyte of fixture data.
	huge, err := os.Create(filepath.Join(mediaDir, strings.Repeat("f", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err = huge.Truncate(clawBotDeliveryMediaQuota); err != nil {
		t.Fatal(err)
	}
	_ = huge.Close()
	if _, err = m.SaveDeliveryMedia(account, []byte("new")); err == nil {
		t.Fatal("quota ignored")
	}
}
