package api

import (
	"bytes"
	"cornerstone/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestPromptManager(t *testing.T) *storage.PromptManager {
	t.Helper()

	pm := storage.NewPromptManager(filepath.Join(t.TempDir(), "prompts"))
	if _, err := pm.Create("prompt-1", "Alice", "system prompt", "old note", ""); err != nil {
		t.Fatalf("Create prompt failed: %v", err)
	}
	return pm
}

func TestHandlePromptByID_CanClearDescriptionExplicitly(t *testing.T) {
	handler := &Handler{promptManager: newTestPromptManager(t)}

	req := httptest.NewRequest(
		http.MethodPut,
		"/management/prompts/prompt-1",
		bytes.NewBufferString(`{"name":"Alice","content":"system prompt","description":""}`),
	)
	rec := httptest.NewRecorder()

	handler.handlePromptByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated, ok := handler.promptManager.Get("prompt-1")
	if !ok {
		t.Fatal("prompt not found after update")
	}
	if updated.Description != "" {
		t.Fatalf("description = %q, want empty string", updated.Description)
	}
}

func TestHandlePromptByID_PreservesDescriptionWhenOmitted(t *testing.T) {
	handler := &Handler{promptManager: newTestPromptManager(t)}

	req := httptest.NewRequest(
		http.MethodPut,
		"/management/prompts/prompt-1",
		bytes.NewBufferString(`{"name":"Alice Updated","content":"system prompt updated"}`),
	)
	rec := httptest.NewRecorder()

	handler.handlePromptByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated, ok := handler.promptManager.Get("prompt-1")
	if !ok {
		t.Fatal("prompt not found after update")
	}
	if updated.Description != "old note" {
		t.Fatalf("description = %q, want %q", updated.Description, "old note")
	}
}
