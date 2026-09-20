package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentCleanupPreservesAllDurableOwners(t *testing.T) {
	dir := t.TempDir()
	store := AttachmentStore{Dir: filepath.Join(dir, "attachments")}
	now := time.Now()
	saved := make(map[string]ChatAttachment)
	for _, owner := range []string{"history", "queue", "reference", "tool", "orphan", "fresh"} {
		a, err := store.Save([]byte(owner), owner+".bin", "file")
		if err != nil {
			t.Fatal(err)
		}
		saved[owner] = a
		if owner != "fresh" {
			id, _ := attachmentID(a.Path)
			old := now.Add(-48 * time.Hour)
			if err := os.Chtimes(filepath.Join(store.Dir, id), old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshots := map[string]interface{}{
		"chats/session.jsonl": ChatMessage{Attachments: []ChatAttachment{saved["history"]}},
		// SourcePath must protect bytes even when the job has no history metadata.
		"clawbot/state.json":            map[string]interface{}{"chunks": []interface{}{map[string]interface{}{"media": map[string]string{"source_path": saved["queue"].Path}}}},
		"clawbot/references/items.json": map[string]string{"reference": saved["reference"].Path},
		"tools.json":                    map[string]string{"content": `{"path":"` + saved["tool"].Path + `"}`},
	}
	for name, value := range snapshots {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		b, _ := json.Marshal(value)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	n, err := CleanupAttachmentsBeforeStart(dir, now)
	if err != nil || n != 1 {
		t.Fatalf("removed %d: %v", n, err)
	}
	for owner, a := range saved {
		_, err := store.Read(a.Path)
		if (err != nil) != (owner == "orphan") {
			t.Fatalf("%s: %v", owner, err)
		}
	}
	// Deleted history is reclaimed on a subsequent startup, not while queue
	// workers or legacy pending messages might still be accessing the bytes.
	os.Remove(filepath.Join(dir, "chats/session.jsonl"))
	n, err = CleanupAttachmentsBeforeStart(dir, now)
	if err != nil || n != 1 {
		t.Fatalf("removed %d: %v", n, err)
	}
}

func TestAttachmentCleanupFailsClosed(t *testing.T) {
	for _, mode := range []string{"corrupt", "symlink", "escaped"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			store := AttachmentStore{Dir: filepath.Join(dir, "attachments")}
			a, err := store.Save([]byte("keep"), "keep.bin", "file")
			if err != nil {
				t.Fatal(err)
			}
			id, _ := attachmentID(a.Path)
			old := time.Now().Add(-48 * time.Hour)
			os.Chtimes(filepath.Join(store.Dir, id), old, old)
			switch mode {
			case "corrupt":
				os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"broken":`), 0600)
			case "symlink":
				os.Symlink(t.TempDir(), filepath.Join(dir, "linked"))
			case "escaped":
				// JSON escaping must not turn a live path into an apparent orphan.
				b := `{"path":"\u002fapi\u002fattachments\u002f` + id + `"}`
				os.WriteFile(filepath.Join(dir, "state.json"), []byte(b), 0600)
			}
			n, err := CleanupAttachmentsBeforeStart(dir, time.Now())
			if n != 0 || (mode != "escaped" && err == nil) || (mode == "escaped" && err != nil) {
				t.Fatalf("%d %v", n, err)
			}
			if _, err := store.Read(a.Path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnknownMediaPreservesOriginalBytes(t *testing.T) {
	store := AttachmentStore{Dir: t.TempDir()}
	for _, kind := range []string{"audio", "video"} {
		a, err := store.Save([]byte("unknown original codec"), kind+".bin", kind)
		if err != nil || a.MIMEType != "application/octet-stream" {
			t.Fatalf("%+v %v", a, err)
		}
		b, err := store.Read(a.Path)
		if err != nil || string(b) != "unknown original codec" {
			t.Fatalf("%q %v", b, err)
		}
	}
}

func TestAttachmentCleanupInterruptedSaves(t *testing.T) {
	dir := t.TempDir()
	attachments := filepath.Join(dir, "attachments")
	os.MkdirAll(attachments, 0700)
	now := time.Now()
	for _, name := range []string{".incoming-123456", ".incoming-654321", "unexpected"} {
		p := filepath.Join(attachments, name)
		os.WriteFile(p, []byte("partial"), 0600)
		if name != ".incoming-654321" {
			os.Chtimes(p, now.Add(-48*time.Hour), now.Add(-48*time.Hour))
		}
	}
	n, err := CleanupAttachmentsBeforeStart(dir, now)
	if err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	entries, _ := os.ReadDir(attachments)
	if len(entries) != 2 {
		t.Fatal(entries)
	}
}
