package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAttachmentStoreAndHistory(t *testing.T) {
	store := AttachmentStore{Dir: filepath.Join(t.TempDir(), "attachments")}
	a, err := store.Save([]byte("hello"), "notes.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Read(a.Path)
	if err != nil || string(b) != "hello" || a.Size != 5 {
		t.Fatalf("%s %v %+v", b, err, a)
	}
	cm := newDeliveryTestManager(t)
	if err := cm.CommitDeliveryBatch("delivery", "attachment", []ChatMessage{{Role: "user", Content: "attachment", Attachments: []ChatAttachment{a}, Timestamp: time.Now()}}, false, nil); err != nil {
		t.Fatal(err)
	}
	record, _ := cm.GetSession("delivery")
	record.Messages[0].Attachments[0].Name = "mutated"
	record, _ = cm.GetSession("delivery")
	if record.Messages[0].Attachments[0].Name != a.Name {
		t.Fatal("snapshot aliases attachments")
	}
	restored := NewChatManager(cm.dataDir)
	record, _ = restored.GetSession("delivery")
	if record.Messages[0].Attachments[0] != a {
		t.Fatal("attachment metadata did not survive restart")
	}
	if err := store.Remove(a.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(a.Path); err == nil {
		t.Fatal("removed attachment remains")
	}
}
func TestAttachmentValidationAndCleanup(t *testing.T) {
	store := AttachmentStore{Dir: t.TempDir()}
	for _, name := range []string{"../secret", `a\b`, "evil\nname", "", ".", strings.Repeat("a", 241)} {
		if _, err := store.Save([]byte("hello"), name, "file"); err == nil {
			t.Fatal(name)
		}
	}
	entries, _ := os.ReadDir(store.Dir)
	if len(entries) != 0 {
		t.Fatal("failed saves left debris")
	}
	for _, path := range []string{"../secret", "/api/attachments/../secret", "/api/attachments/not-an-id"} {
		if _, err := store.Open(path); err == nil {
			t.Fatal(path)
		}
	}
	a, err := store.Save([]byte("hello"), "safe.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := attachmentID(a.Path)
	os.Remove(filepath.Join(store.Dir, id))
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(store.Dir, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(a.Path); err == nil {
		t.Fatal("symlink escape")
	}
}
