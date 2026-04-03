package api

import (
	"bytes"
	"cornerstone/client"
	"cornerstone/storage"
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
