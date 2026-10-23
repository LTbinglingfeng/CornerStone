package storage

import (
	"bytes"
	"cornerstone/client"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestMemoryExtractor_MissingMatchingIDRejectsEntireBatch(t *testing.T) {
	for _, id := range []string{"missing", "deleted", "", "../invalid"} {
		t.Run(id, func(t *testing.T) {
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			existingID := "existing"
			env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
				makeMemoryBatchUpsertToolCall(t, "batch", []ExtractedMemory{
					{Subject: SubjectUser, Category: CategoryFact, Content: "new fact"},
					{MatchingID: &existingID, Subject: SubjectUser, Category: CategoryFact, Content: "updated fact"},
					{MatchingID: &id, Subject: SubjectUser, Category: CategoryFact, Content: "must not resurrect"},
				}),
			}), fixedTimeProvider{now: now})
			if err := env.mm.Add(env.promptID, Memory{ID: existingID, Content: "original", Strength: .8, SeenCount: 1}); err != nil {
				t.Fatal(err)
			}
			if id == "deleted" {
				if err := env.mm.Add(env.promptID, Memory{ID: id, Content: "deleted fact"}); err != nil {
					t.Fatal(err)
				}
				if err := env.mm.Delete(env.promptID, id); err != nil {
					t.Fatal(err)
				}
			}
			before := env.mm.GetAll(env.promptID)
			beforeDisk, err := os.ReadFile(env.mm.getMemoryPath(env.promptID))
			if err != nil {
				t.Fatal(err)
			}
			err = env.extractor.ExtractAndSave(env.promptID, env.sessionID)
			if id == "missing" || id == "deleted" {
				if !errors.Is(err, errMemoryMatchingIDNotFound) {
					t.Fatalf("error = %v, want missing matching_id", err)
				}
			} else if !errors.Is(err, ErrInvalidID) {
				t.Fatalf("error = %v, want invalid matching_id", err)
			}
			if got := env.mm.GetAll(env.promptID); !reflect.DeepEqual(got, before) {
				t.Fatalf("rejected batch partly changed memory: %+v", got)
			}
			if got, err := os.ReadFile(env.mm.getMemoryPath(env.promptID)); err != nil || !bytes.Equal(got, beforeDisk) {
				t.Fatalf("rejected batch changed file: err=%v", err)
			}
		})
	}
}

func TestMemoryExtractor_SaveFailurePropagatesAndRetryDoesNotPartiallyReapply(t *testing.T) {
	existingID := "existing"
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "batch", []ExtractedMemory{
			{MatchingID: &existingID, Subject: SubjectUser, Category: CategoryFact, Content: "updated fact"},
			{Subject: SubjectUser, Category: CategoryPreference, Content: "likes tea"},
		}),
	}), fixedTimeProvider{now: time.Now()})
	if err := env.mm.Add(env.promptID, Memory{ID: existingID, Content: "original", Strength: .8, SeenCount: 1}); err != nil {
		t.Fatal(err)
	}
	before := env.mm.GetAll(env.promptID)
	path := env.mm.getMemoryPath(env.promptID)
	if err := os.Rename(path, path+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	err := env.extractor.ExtractAndSave(env.promptID, env.sessionID)
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if !errors.As(err, &pathErr) && !errors.As(err, &linkErr) {
		t.Fatalf("error must preserve underlying filesystem failure: %v", err)
	}
	if got := env.mm.GetAll(env.promptID); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed batch partly applied: %+v", got)
	}
	assertNoMemoryTempFiles(t, env.mm, env.promptID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".backup", path); err != nil {
		t.Fatal(err)
	}
	if err := env.extractor.ExtractAndSave(env.promptID, env.sessionID); err != nil {
		t.Fatal(err)
	}
	got := env.mm.GetAll(env.promptID)
	if len(got) != 2 || got[0].SeenCount != 2 || got[0].Content != "updated fact" {
		t.Fatalf("retry duplicated an add or reinforcement: %+v", got)
	}
	reloaded, err := NewMemoryManager(env.mm.baseDir).Load(env.promptID)
	if err != nil || len(reloaded) != 2 || reloaded[0].SeenCount != 2 {
		t.Fatalf("committed batch not durable: %+v, err=%v", reloaded, err)
	}
}

func TestMemoryExtractionApplyStats_UnprocessedItemsAreAccountedFor(t *testing.T) {
	for _, failure := range []string{"first_missing", "middle_missing", "load_failure"} {
		t.Run(failure, func(t *testing.T) {
			mm := NewMemoryManager(t.TempDir())
			extractor := &MemoryExtractor{mm: mm}
			missing := "missing"
			add := ExtractedMemory{Subject: SubjectUser, Category: CategoryFact, Content: "fact"}
			update := ExtractedMemory{MatchingID: &missing, Subject: SubjectUser, Category: CategoryFact, Content: "fact"}
			items := []ExtractedMemory{update, add, add}
			wantUnprocessed := 2
			if failure == "middle_missing" {
				items = []ExtractedMemory{add, update, add}
				wantUnprocessed = 1
			}
			if failure == "load_failure" {
				if err := os.MkdirAll(mm.getMemoryPath("persona"), 0700); err != nil {
					t.Fatal(err)
				}
				wantUnprocessed = 3
			}
			stats, err := extractor.applyExtractedMemories("persona", items, time.Now())
			if err == nil || stats.UnprocessedCount != wantUnprocessed {
				t.Fatalf("stats=%+v err=%v", stats, err)
			}
			accounted := stats.AddedCount + stats.UpdatedCount + stats.InvalidCount + stats.AddFailedCount + stats.UpdateFailedCount + stats.UnprocessedCount
			if accounted != stats.InputCount || stats.AddedCount != 0 || stats.UpdatedCount != 0 {
				t.Fatalf("unaccounted or falsely committed items: %+v", stats)
			}
		})
	}
}

func TestMemoryExtractionApplyStats_ReportOnlyCommittedChanges(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	extractor := &MemoryExtractor{mm: mm}
	missingID := "missing"
	stats, err := extractor.applyExtractedMemories("persona", []ExtractedMemory{
		{Subject: SubjectUser, Category: CategoryFact, Content: "new fact"},
		{MatchingID: &missingID, Subject: SubjectUser, Category: CategoryFact, Content: "stale update"},
	}, time.Now())
	if !errors.Is(err, errMemoryMatchingIDNotFound) {
		t.Fatal(err)
	}
	if stats.AddedCount != 0 || stats.UpdatedCount != 0 || stats.AddFailedCount != 1 || stats.UpdateFailedCount != 1 {
		t.Fatalf("rolled back changes counted as saved: %+v", stats)
	}
	if len(mm.GetAll("persona")) != 0 {
		t.Fatal("rejected batch published memory")
	}
}
