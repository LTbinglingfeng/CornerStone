package storage

import (
	"cornerstone/client"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemorySession_ReadsCurrentManagerStateAcrossSessions(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	first := NewMemorySession("persona", "web", mm, nil)
	second := NewMemorySession("persona", "wechat", mm, nil)
	if err := mm.Add("persona", Memory{ID: "fact", Subject: SubjectUser, Category: CategoryFact, Content: "old fact", Strength: .9}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*MemorySession{first, second} {
		if got := s.GetActiveMemories(); len(got) != 1 || got[0].Content != "old fact" {
			t.Fatalf("new memory invisible without refresh: %+v", got)
		}
	}
	content := "corrected fact"
	if err := mm.Patch("persona", MemoryPatch{ID: "fact", Content: &content}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*MemorySession{first, second} {
		got := s.GetActiveMemories()
		if len(got) != 1 || got[0].Content != content {
			t.Fatalf("updated memory invisible without refresh: %+v", got)
		}
		got[0].Content = "caller mutation"
	}
	if mm.FindByID("persona", "fact").Content != content {
		t.Fatal("caller mutated manager-owned memory")
	}
	if err := mm.Delete("persona", "fact"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*MemorySession{first, second} {
		if got := s.GetActiveMemories(); len(got) != 0 {
			t.Fatalf("deleted memory remains visible: %+v", got)
		}
		if prompt := BuildPromptWithMemory("base", s.GetActiveMemories()); prompt != "base" {
			t.Fatalf("deleted fact injected into prompt: %q", prompt)
		}
	}
}

func TestMemorySession_ActiveSelectionIsRecomputed(t *testing.T) {
	mm := NewMemoryManager(t.TempDir())
	if err := mm.Add("persona", Memory{ID: "fact", Subject: SubjectUser, Content: "fact", Strength: .9}); err != nil {
		t.Fatal(err)
	}
	s := NewMemorySession("persona", "conversation", mm, nil)
	if len(s.GetActiveMemories()) != 1 {
		t.Fatal("fresh memory missing")
	}
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := mm.Patch("persona", MemoryPatch{ID: "fact", LastSeen: &old}); err != nil {
		t.Fatal(err)
	}
	if len(s.GetActiveMemories()) != 0 {
		t.Fatal("selection retained a memory that is now below the active threshold")
	}
	pinned := true
	if err := mm.Patch("persona", MemoryPatch{ID: "fact", Pinned: &pinned}); err != nil {
		t.Fatal(err)
	}
	if len(s.GetActiveMemories()) != 1 {
		t.Fatal("newly pinned memory was not selected")
	}
}

func TestMemorySession_MissingMatchingIDDoesNotRetryAsAnAdd(t *testing.T) {
	missing := "deleted"
	response := makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "batch", []ExtractedMemory{
			{MatchingID: &missing, Subject: SubjectUser, Category: CategoryFact, Content: "old fact"},
		}),
	})
	env := newMemoryExtractorTestEnv(t, response, fixedTimeProvider{now: time.Now()})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	cfg := env.extractor.configMgr.Get()
	cfg.MemoryProvider.BaseURL = server.URL
	if err := env.extractor.configMgr.Update(cfg); err != nil {
		t.Fatal(err)
	}
	s := NewMemorySession(env.promptID, env.sessionID, env.mm, env.extractor)
	if err := s.refreshWithExtractRetry(1); !errors.Is(err, errMemoryMatchingIDNotFound) {
		t.Fatalf("error = %v, want missing matching_id", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("deterministic conflict retried %d times", calls.Load())
	}
	if len(s.GetActiveMemories()) != 0 {
		t.Fatal("deleted target was resurrected")
	}
}

func TestMemorySession_SaveFailureIsReturned(t *testing.T) {
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "batch", []ExtractedMemory{{Subject: SubjectUser, Category: CategoryFact, Content: "fact"}}),
	}), fixedTimeProvider{now: time.Now()})
	env.mm.GetAll(env.promptID)
	if err := os.Mkdir(env.mm.getMemoryPath(env.promptID), 0700); err != nil {
		t.Fatal(err)
	}
	s := NewMemorySession(env.promptID, env.sessionID, env.mm, env.extractor)
	if err := s.refreshWithExtractRetry(0); err == nil || !strings.Contains(err.Error(), "记忆保存失败") {
		t.Fatalf("session swallowed save error: %v", err)
	}
	if len(s.GetActiveMemories()) != 0 {
		t.Fatal("failed memory appeared in next prompt")
	}
}
