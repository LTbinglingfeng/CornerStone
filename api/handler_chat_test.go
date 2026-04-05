package api

import (
	"bytes"
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type flushRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		header: make(http.Header),
		code:   http.StatusOK,
	}
}

func (f *flushRecorder) Header() http.Header {
	return f.header
}

func (f *flushRecorder) WriteHeader(statusCode int) {
	f.code = statusCode
}

func (f *flushRecorder) Write(p []byte) (int, error) {
	return f.body.Write(p)
}

func (f *flushRecorder) Flush() {}

func TestHandleNormalChat_PersistsToolMessagesOnToolLoopError(t *testing.T) {
	chatMgr := storage.NewChatManager(t.TempDir())
	h := &Handler{chatManager: chatMgr}
	sessionID := "test-session"

	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("tmp", toolCall("call_pat", "send_pat", `{"name":"Alice","target":"Bob"}`))),
			// Next hop fails (no more responses) after tool execution.
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/chat", nil)
	h.handleNormalChat(
		w,
		r,
		ai,
		client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}},
		sessionID,
		true,
		nil,
		"",
		"",
		nil,
	)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	record, ok := chatMgr.GetSession(sessionID)
	if !ok {
		t.Fatalf("session %q not found", sessionID)
	}
	if len(record.Messages) != 2 {
		t.Fatalf("messages len=%d, want 2", len(record.Messages))
	}
	if record.Messages[0].Role != "assistant" || len(record.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected assistant(tool_calls) message, got=%#v", record.Messages[0])
	}
	if record.Messages[1].Role != "tool" || record.Messages[1].ToolCallID != "call_pat" {
		t.Fatalf("expected tool message call_pat, got=%#v", record.Messages[1])
	}
}

func TestHandleStreamChat_PersistsToolMessagesOnToolLoopError(t *testing.T) {
	chatMgr := storage.NewChatManager(t.TempDir())
	h := &Handler{chatManager: chatMgr}
	sessionID := "test-session"

	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("tmp", toolCall("call_pat", "send_pat", `{"name":"Alice","target":"Bob"}`))),
			// Next hop fails (no more responses) after tool execution.
		},
	}

	w := newFlushRecorder()
	r := httptest.NewRequest("POST", "/api/chat", nil)
	h.handleStreamChat(
		w,
		r,
		ai,
		client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}},
		sessionID,
		true,
		nil,
		"",
		"",
		nil,
	)

	record, ok := chatMgr.GetSession(sessionID)
	if !ok {
		t.Fatalf("session %q not found", sessionID)
	}
	if len(record.Messages) != 2 {
		t.Fatalf("messages len=%d, want 2", len(record.Messages))
	}
	if record.Messages[0].Role != "assistant" || len(record.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected assistant(tool_calls) message, got=%#v", record.Messages[0])
	}
	if record.Messages[1].Role != "tool" || record.Messages[1].ToolCallID != "call_pat" {
		t.Fatalf("expected tool message call_pat, got=%#v", record.Messages[1])
	}
}

func TestGetOrCreateMemorySession_ScopesByPromptID(t *testing.T) {
	configManager := newTestProviderConfigManager(t, newTestProvider("provider-1"))
	cfg := configManager.Get()
	cfg.MemoryEnabled = true
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	handler := &Handler{
		configManager:  configManager,
		memoryManager:  storage.NewMemoryManager(t.TempDir()),
		memorySessions: make(map[string]*storage.MemorySession),
		cleanupDone:    make(chan struct{}),
	}

	first := handler.getOrCreateMemorySession("prompt_a", "session_1")
	second := handler.getOrCreateMemorySession("prompt_b", "session_1")
	if first == nil || second == nil {
		t.Fatalf("expected memory sessions, got first=%v second=%v", first, second)
	}
	if first == second {
		t.Fatal("sessions with different prompt IDs should not reuse the same MemorySession")
	}
	if len(handler.memorySessions) != 2 {
		t.Fatalf("memorySessions len=%d, want 2", len(handler.memorySessions))
	}
	if _, ok := handler.memorySessions[memorySessionCacheKey("session_1", "prompt_a")]; !ok {
		t.Fatal("expected prompt_a scoped session to be cached")
	}
	if _, ok := handler.memorySessions[memorySessionCacheKey("session_1", "prompt_b")]; !ok {
		t.Fatal("expected prompt_b scoped session to be cached")
	}
}

func TestChatToolExecutor_WriteMemoryRefreshesSessionAndReinforcesDuplicates(t *testing.T) {
	memoryManager := storage.NewMemoryManager(t.TempDir())
	memSession := storage.NewMemorySession("prompt_a", "session_1", memoryManager, nil)

	executor := newChatToolExecutor(nil, nil)
	executor.memoryManager = memoryManager

	firstRaw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call_memory_1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "write_memory",
			Arguments: `{"items":[{"subject":"user","category":"preference","content":"用户喜欢黑咖啡"}]}`,
		},
	}, chatToolContext{
		PromptID:   "prompt_a",
		MemSession: memSession,
	})

	var firstResult chatToolResult
	if err := json.Unmarshal([]byte(firstRaw), &firstResult); err != nil {
		t.Fatalf("Unmarshal first tool result failed: %v", err)
	}
	if !firstResult.OK {
		t.Fatalf("first result error=%q", firstResult.Error)
	}

	memories := memoryManager.GetAll("prompt_a")
	if len(memories) != 1 {
		t.Fatalf("memories len=%d, want 1", len(memories))
	}
	if memories[0].SeenCount != 1 {
		t.Fatalf("first seen_count=%d, want 1", memories[0].SeenCount)
	}

	active := memSession.GetActiveMemories()
	if len(active) != 1 || active[0].Content != "用户喜欢黑咖啡" {
		t.Fatalf("active memories=%#v, want immediate refresh with new memory", active)
	}

	secondRaw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call_memory_2",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "write_memory",
			Arguments: `{"items":[{"subject":"user","category":"preference","content":"用户喜欢黑咖啡"}]}`,
		},
	}, chatToolContext{
		PromptID:   "prompt_a",
		MemSession: memSession,
	})

	var secondResult chatToolResult
	if err := json.Unmarshal([]byte(secondRaw), &secondResult); err != nil {
		t.Fatalf("Unmarshal second tool result failed: %v", err)
	}
	if !secondResult.OK {
		t.Fatalf("second result error=%q", secondResult.Error)
	}

	memories = memoryManager.GetAll("prompt_a")
	if len(memories) != 1 {
		t.Fatalf("memories len after duplicate=%d, want 1", len(memories))
	}
	if memories[0].SeenCount != 2 {
		t.Fatalf("seen_count after duplicate=%d, want 2", memories[0].SeenCount)
	}
	if memories[0].Strength <= storage.DefaultStrengthForCategory(storage.CategoryPreference) {
		t.Fatalf("strength after duplicate=%v, want reinforced value", memories[0].Strength)
	}

	blockedRaw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call_memory_3",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "write_memory",
			Arguments: `{"items":[{"subject":"user","category":"fact","content":"我的手机号是13800138000"}]}`,
		},
	}, chatToolContext{
		PromptID:   "prompt_a",
		MemSession: memSession,
	})

	var blockedResult chatToolResult
	if err := json.Unmarshal([]byte(blockedRaw), &blockedResult); err != nil {
		t.Fatalf("Unmarshal blocked tool result failed: %v", err)
	}
	if !blockedResult.OK {
		t.Fatalf("blocked result should still be ok with blocked stats, error=%q", blockedResult.Error)
	}
	memories = memoryManager.GetAll("prompt_a")
	if len(memories) != 1 {
		t.Fatalf("blocked write should not add memory, len=%d", len(memories))
	}
}
