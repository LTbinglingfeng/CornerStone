package storage

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func assertNoMemoryTempFiles(t *testing.T, mm *MemoryManager, promptID string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(mm.baseDir, promptID, ".memories-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, err=%v", matches, err)
	}
}

func TestMemoryMutations_FailedReplacementPreservesCache(t *testing.T) {
	operations := map[string]func(*MemoryManager) error{
		"add": func(mm *MemoryManager) error {
			return mm.Add("persona", Memory{ID: "new", Content: "new fact", Strength: .8})
		},
		"update": func(mm *MemoryManager) error {
			return mm.Update("persona", Memory{ID: "first", Content: "changed", Strength: .8})
		},
		"patch": func(mm *MemoryManager) error {
			content := "changed"
			return mm.Patch("persona", MemoryPatch{ID: "first", Content: &content})
		},
		"delete": func(mm *MemoryManager) error { return mm.Delete("persona", "first") },
		"delete_batch": func(mm *MemoryManager) error {
			n, err := mm.DeleteBatch("persona", []string{"first", "last"})
			if err != nil && n != 0 {
				t.Errorf("failed delete reported %d committed removals", n)
			}
			return err
		},
		"delete_archived": func(mm *MemoryManager) error {
			n, err := mm.DeleteArchived("persona")
			if err != nil && n != 0 {
				t.Errorf("failed delete reported %d committed removals", n)
			}
			return err
		},
		"save": func(mm *MemoryManager) error { return mm.Save("persona") },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			mm := NewMemoryManager(t.TempDir())
			for _, id := range []string{"first", "middle", "last"} {
				if err := mm.Add("persona", Memory{ID: id, Content: id, Strength: .1}); err != nil {
					t.Fatal(err)
				}
			}
			before := mm.GetAll("persona")
			// Also retain the old backing array: failed writes must not mutate it.
			published := mm.cache["persona"]
			path := mm.getMemoryPath("persona")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			backup := path + ".backup"
			if err := os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			// A directory at the target makes rename fail, including under root.
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			if err := operation(mm); err == nil {
				t.Fatal("expected persistence error")
			}
			if got := mm.GetAll("persona"); !reflect.DeepEqual(got, before) {
				t.Fatalf("failed write changed cache: got=%+v want=%+v", got, before)
			}
			if !reflect.DeepEqual(published, before) {
				t.Fatal("failed write mutated previously published backing array")
			}
			assertNoMemoryTempFiles(t, mm, "persona")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, path); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) {
				t.Fatalf("original file changed: err=%v", err)
			}
			if err := operation(mm); err != nil {
				t.Fatalf("retry after fixing filesystem: %v", err)
			}
			reloaded, err := NewMemoryManager(mm.baseDir).Load("persona")
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, _ := json.Marshal(mm.GetAll("persona"))
			gotJSON, _ := json.Marshal(reloaded)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("published cache differs from disk: got=%s want=%s", gotJSON, wantJSON)
			}
		})
	}
}

func TestMemorySave_EncodingFailureDoesNotTruncateOriginal(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	if err := mm.Add("persona", Memory{ID: "existing", Content: strings.Repeat("x", 8192), Strength: .9}); err != nil {
		t.Fatal(err)
	}
	path := mm.getMemoryPath("persona")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The first row exceeds the write buffer; the next row cannot be encoded.
	// This exercises failure after the candidate file already received bytes.
	if err := mm.Add("persona", Memory{ID: "invalid", Stability: math.NaN()}); err == nil {
		t.Fatal("expected JSON encoding failure")
	}
	if got := mm.GetAll("persona"); len(got) != 1 || got[0].ID != "existing" {
		t.Fatalf("failed add leaked into cache: %+v", got)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("encoding failure damaged original file: err=%v", err)
	}
	assertNoMemoryTempFiles(t, mm, "persona")
	if err := mm.Add("persona", Memory{ID: "valid", Strength: .8}); err != nil {
		t.Fatalf("invalid entry poisoned later writes: %v", err)
	}
}

func TestMemorySave_SuccessReplacesFileAndKeepsPrivatePermissions(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	if err := mm.Add("persona", Memory{ID: "existing", Strength: .9}); err != nil {
		t.Fatal(err)
	}
	path := mm.getMemoryPath("persona")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content := "changed"
	if err := mm.Patch("persona", MemoryPatch{ID: "existing", Content: &content}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("save rewrote the original file instead of replacing it")
	}
	if runtime.GOOS != "windows" && after.Mode().Perm() != 0600 {
		t.Fatalf("memory permissions = %o, want 600", after.Mode().Perm())
	}
	assertNoMemoryTempFiles(t, mm, "persona")
}

func TestMemorySave_FailedInitialWriteDoesNotPublishEntry(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	mm.GetAll("persona")
	if err := os.WriteFile(filepath.Join(mm.baseDir, "persona"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := mm.Add("persona", Memory{ID: "new", CreatedAt: time.Now()}); err == nil {
		t.Fatal("expected directory creation failure")
	}
	if got := mm.GetAll("persona"); len(got) != 0 {
		t.Fatalf("failed first write was published: %+v", got)
	}
}
