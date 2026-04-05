package api

import (
	"bytes"
	"cornerstone/storage"
	"encoding/json"
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

func TestHandlePrompts_IncludesRecoveredPromptFromSessions(t *testing.T) {
	pm := newTestPromptManager(t)
	cm := storage.NewChatManager(filepath.Join(t.TempDir(), "chats"))
	if _, err := cm.CreateSession("session-1", "Recovered Chat", "ghost-prompt", "Ghost Persona"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	handler := &Handler{promptManager: pm, chatManager: cm}
	req := httptest.NewRequest(http.MethodGet, "/management/prompts", nil)
	rec := httptest.NewRecorder()

	handler.handlePrompts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool             `json:"success"`
		Data    []storage.Prompt `json:"data"`
		Error   string           `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("success = false, error = %q", resp.Error)
	}

	foundRecovered := false
	for _, prompt := range resp.Data {
		if prompt.ID != "ghost-prompt" {
			continue
		}
		foundRecovered = true
		if prompt.Name != "Ghost Persona" {
			t.Fatalf("prompt.Name = %q, want %q", prompt.Name, "Ghost Persona")
		}
	}
	if !foundRecovered {
		t.Fatal("recovered prompt not returned in prompt list")
	}
}

func TestHandlePromptByID_ReturnsRecoveredPromptFromSessions(t *testing.T) {
	pm := newTestPromptManager(t)
	cm := storage.NewChatManager(filepath.Join(t.TempDir(), "chats"))
	if _, err := cm.CreateSession("session-1", "Recovered Chat", "ghost-prompt", "Ghost Persona"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	handler := &Handler{promptManager: pm, chatManager: cm}
	req := httptest.NewRequest(http.MethodGet, "/management/prompts/ghost-prompt", nil)
	rec := httptest.NewRecorder()

	handler.handlePromptByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool           `json:"success"`
		Data    storage.Prompt `json:"data"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("success = false, error = %q", resp.Error)
	}
	if resp.Data.ID != "ghost-prompt" {
		t.Fatalf("prompt.ID = %q, want %q", resp.Data.ID, "ghost-prompt")
	}
	if resp.Data.Name != "Ghost Persona" {
		t.Fatalf("prompt.Name = %q, want %q", resp.Data.Name, "Ghost Persona")
	}
}

func TestHandlePromptByID_UpdateCreatesRecoveredPromptWhenMissing(t *testing.T) {
	pm := newTestPromptManager(t)
	cm := storage.NewChatManager(filepath.Join(t.TempDir(), "chats"))
	if _, err := cm.CreateSession("session-1", "Recovered Chat", "ghost-prompt", "Ghost Persona"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	handler := &Handler{promptManager: pm, chatManager: cm}
	req := httptest.NewRequest(
		http.MethodPut,
		"/management/prompts/ghost-prompt",
		bytes.NewBufferString(`{"name":"Ghost Persona","content":"restored system prompt","description":"restored note"}`),
	)
	rec := httptest.NewRecorder()

	handler.handlePromptByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	created, ok := handler.promptManager.Get("ghost-prompt")
	if !ok {
		t.Fatal("prompt not persisted after recovery update")
	}
	if created.Name != "Ghost Persona" {
		t.Fatalf("name = %q, want %q", created.Name, "Ghost Persona")
	}
	if created.Content != "restored system prompt" {
		t.Fatalf("content = %q, want %q", created.Content, "restored system prompt")
	}
	if created.Description != "restored note" {
		t.Fatalf("description = %q, want %q", created.Description, "restored note")
	}
}
