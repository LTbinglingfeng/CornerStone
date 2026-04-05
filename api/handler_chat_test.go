package api

import (
	"bytes"
	"context"
	"cornerstone/client"
	"cornerstone/exacttime"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestChatToolExecutor_ScheduleReminderCreatesWebReminder(t *testing.T) {
	chatMgr := storage.NewChatManager(t.TempDir())
	if _, err := chatMgr.CreateSession("session-1", "Alice", "prompt-1", "Alice"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	handler := &Handler{
		chatManager:   chatMgr,
		promptManager: newTestPromptManager(t),
	}
	reminderSvc := NewReminderService(handler, storage.NewReminderManager(filepath.Join(t.TempDir(), "reminders")), &stubExactTimeService{
		now: time.Date(2026, 4, 5, 11, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	})
	handler.SetReminderService(reminderSvc)

	executor := newChatToolExecutor(nil, nil)
	executor.reminderService = reminderSvc

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-reminder-1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "schedule_reminder",
			Arguments: `{"due_at":"2026-04-05T19:30:00+08:00","title":"喝水提醒","reminder_prompt":"到时间后提醒用户喝水。"}`,
		},
	}, chatToolContext{
		SessionID:  "session-1",
		PromptID:   "prompt-1",
		PromptName: "Alice",
		Channel:    chatToolChannelDefault,
	})

	var result chatToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, error=%q", result.Error)
	}

	reminders := reminderSvc.List()
	if len(reminders) != 1 {
		t.Fatalf("reminders len = %d, want 1", len(reminders))
	}
	if reminders[0].Channel != storage.ReminderChannelWeb {
		t.Fatalf("channel = %q, want %q", reminders[0].Channel, storage.ReminderChannelWeb)
	}
	if reminders[0].SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", reminders[0].SessionID, "session-1")
	}
}

func TestReminderService_FireReminder_WebPersistsAssistantMessageWithoutInternalPrompt(t *testing.T) {
	var state struct {
		req struct {
			Messages []struct {
				Role    string      `json:"role"`
				Content interface{} `json:"content"`
			} `json:"messages"`
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&state.req); err != nil {
			t.Fatalf("decode chat request failed: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.ChatResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "gpt-test",
			Choices: []client.Choice{
				{
					Index: 0,
					Message: client.Message{
						Role:    "assistant",
						Content: "该喝水了。",
					},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	provider := newTestProvider("provider-1")
	provider.BaseURL = server.URL
	configManager := newTestProviderConfigManager(t, provider)
	promptManager := newTestPromptManager(t)
	if _, err := promptManager.Update("prompt-1", "Alice", "latest persona", "old note", ""); err != nil {
		t.Fatalf("Update prompt failed: %v", err)
	}

	chatMgr := storage.NewChatManager(t.TempDir())
	if _, err := chatMgr.CreateSession("session-1", "Alice", "prompt-1", "Alice"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := chatMgr.AddMessage("session-1", "user", "记得提醒我喝水"); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	exactTimeSvc := &stubExactTimeService{
		now: time.Date(2026, 4, 5, 19, 30, 0, 0, time.FixedZone("CST", 8*3600)),
		status: exacttime.Status{
			Server:      "ntp.aliyun.com",
			LastSuccess: true,
			Message:     "ntp sync succeeded",
		},
	}
	handler := &Handler{
		configManager: configManager,
		promptManager: promptManager,
		chatManager:   chatMgr,
		userManager:   newTestUserManager(t),
		cleanupDone:   make(chan struct{}),
	}
	handler.SetExactTimeService(exactTimeSvc)
	reminderSvc := NewReminderService(handler, storage.NewReminderManager(filepath.Join(t.TempDir(), "reminders")), exactTimeSvc)
	handler.SetReminderService(reminderSvc)

	reminder, err := reminderSvc.Create(reminderCreateRequest{
		Channel:        storage.ReminderChannelWeb,
		SessionID:      "session-1",
		PromptID:       "prompt-1",
		PromptName:     "Alice",
		Title:          "喝水提醒",
		ReminderPrompt: "到时间后提醒用户喝水，并简单关心一下。",
		DueAt:          time.Date(2026, 4, 5, 19, 29, 0, 0, time.FixedZone("CST", 8*3600)),
	})
	if err != nil {
		t.Fatalf("Create reminder failed: %v", err)
	}

	if err := reminderSvc.fireReminder(context.Background(), reminder.ID); err != nil {
		t.Fatalf("fireReminder failed: %v", err)
	}

	if len(state.req.Messages) < 2 {
		t.Fatalf("request messages len = %d, want >= 2", len(state.req.Messages))
	}
	systemContent, _ := state.req.Messages[0].Content.(string)
	if !strings.Contains(systemContent, "latest persona") {
		t.Fatalf("system prompt = %q, want latest persona content", systemContent)
	}

	record, ok := chatMgr.GetSession("session-1")
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(record.Messages))
	}
	if record.Messages[1].Role != "assistant" || record.Messages[1].Content != "该喝水了。" {
		t.Fatalf("assistant message = %#v, want final reminder reply", record.Messages[1])
	}
	for _, msg := range record.Messages {
		if strings.Contains(msg.Content, "到时间后提醒用户喝水，并简单关心一下。") {
			t.Fatalf("internal reminder prompt leaked into chat history: %#v", record.Messages)
		}
	}

	saved, ok := reminderSvc.Get(reminder.ID)
	if !ok || saved == nil {
		t.Fatal("saved reminder not found")
	}
	if saved.Status != storage.ReminderStatusSent {
		t.Fatalf("status = %q, want %q", saved.Status, storage.ReminderStatusSent)
	}
}

func TestHandleReminderByID_OnlyPendingCanBeEdited(t *testing.T) {
	chatMgr := storage.NewChatManager(t.TempDir())
	if _, err := chatMgr.CreateSession("session-1", "Alice", "prompt-1", "Alice"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	handler := &Handler{
		chatManager:   chatMgr,
		promptManager: newTestPromptManager(t),
	}
	reminderSvc := NewReminderService(handler, storage.NewReminderManager(filepath.Join(t.TempDir(), "reminders")), &stubExactTimeService{
		now: time.Date(2026, 4, 5, 11, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	})
	handler.SetReminderService(reminderSvc)

	reminder, err := reminderSvc.Create(reminderCreateRequest{
		Channel:        storage.ReminderChannelWeb,
		SessionID:      "session-1",
		PromptID:       "prompt-1",
		PromptName:     "Alice",
		Title:          "喝水提醒",
		ReminderPrompt: "到时间后提醒用户喝水。",
		DueAt:          time.Date(2026, 4, 5, 19, 30, 0, 0, time.FixedZone("CST", 8*3600)),
	})
	if err != nil {
		t.Fatalf("Create reminder failed: %v", err)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/settings/reminders/"+reminder.ID, bytes.NewBufferString(`{"title":"新的标题"}`))
	updateRec := httptest.NewRecorder()
	handler.handleReminderByID(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d", updateRec.Code, http.StatusOK)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/settings/reminders/"+reminder.ID+"/cancel", nil)
	cancelRec := httptest.NewRecorder()
	handler.handleReminderByID(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d", cancelRec.Code, http.StatusOK)
	}

	retryReq := httptest.NewRequest(http.MethodPut, "/api/settings/reminders/"+reminder.ID, bytes.NewBufferString(`{"title":"再次修改"}`))
	retryRec := httptest.NewRecorder()
	handler.handleReminderByID(retryRec, retryReq)
	if retryRec.Code != http.StatusConflict {
		t.Fatalf("retry update status = %d, want %d", retryRec.Code, http.StatusConflict)
	}
}
