package api

import (
	"bytes"
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/exacttime"
	"cornerstone/storage"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

func mustJSONRawMessage(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal JSON failed: %v", err)
	}
	return json.RawMessage(data)
}

func writeTinyPNGForNapCatTest(t *testing.T) string {
	t.Helper()

	imageData, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO8B3ioAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatalf("decode png err = %v", err)
	}

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "quote.png")
	if err := os.WriteFile(imagePath, imageData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return imagePath
}

func newNapCatTestService(
	t *testing.T,
	handler *Handler,
	responder func(req napCatActionRequest) napCatActionResponse,
) *NapCatService {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		go func() {
			defer func() {
				_ = conn.Close()
			}()

			for {
				var req napCatActionRequest
				if err := conn.ReadJSON(&req); err != nil {
					return
				}

				resp := responder(req)
				if resp.Status == "" {
					resp.Status = "ok"
				}
				resp.Echo = req.Echo
				if err := conn.WriteJSON(resp); err != nil {
					return
				}
			}
		}()
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("Dial websocket failed: %v", err)
	}

	service := NewNapCatService(handler)
	service.Connect(conn, "test-token")

	t.Cleanup(func() {
		service.Close()
		server.Close()
	})

	return service
}

func newNapCatQuoteTestHandler(t *testing.T) *Handler {
	t.Helper()

	provider := newTestProvider("provider-1")
	provider.APIKey = ""

	return &Handler{
		chatManager:   storage.NewChatManager(t.TempDir()),
		configManager: newTestProviderConfigManager(t, provider),
		cachePhotoDir: t.TempDir(),
	}
}

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

	executor := newChatToolExecutor()
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

	executor := newChatToolExecutor()
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

func TestNapCatParseIncomingPrivateMessage_PureImageUsesPlaceholder(t *testing.T) {
	service := &NapCatService{}
	parsed, ok := service.parseIncomingPrivateMessage(napCatChatSource{
		Kind:   "private",
		SelfID: 20002,
		UserID: 10001,
	}, napCatMessageEvent{
		MessageID: 123,
		Message:   json.RawMessage(`[{"type":"image","data":{"path":"/tmp/test.png"}}]`),
	})
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message for pure image input")
	}
	if parsed.Text != "[用户发送了图片]" {
		t.Fatalf("text = %q, want placeholder", parsed.Text)
	}
	if parsed.MessageID != 123 {
		t.Fatalf("message_id = %d, want 123", parsed.MessageID)
	}
	if len(parsed.ImageSegments) != 1 {
		t.Fatalf("image_segments len = %d, want 1", len(parsed.ImageSegments))
	}
	if parsed.ImageSegments[0].Data.Path != "/tmp/test.png" {
		t.Fatalf("image path = %q, want %q", parsed.ImageSegments[0].Data.Path, "/tmp/test.png")
	}
	if len(parsed.ImagePaths) != 0 {
		t.Fatalf("image_paths len = %d, want 0 before async download", len(parsed.ImagePaths))
	}
}

func TestNapCatProcessIncomingBatch_PersistsReplyQuoteAndText(t *testing.T) {
	handler := newNapCatQuoteTestHandler(t)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}

	var mu sync.Mutex
	getMsgCalls := 0
	getMsgReplyID := int64(0)

	service := newNapCatTestService(t, handler, func(req napCatActionRequest) napCatActionResponse {
		switch req.Action {
		case "get_login_info":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, napCatLoginInfo{UserID: 20002, Nickname: "CornerStone"}),
			}
		case "get_msg":
			mu.Lock()
			getMsgCalls++
			if params, ok := req.Params.(map[string]interface{}); ok {
				if rawID, ok := params["message_id"].(float64); ok {
					getMsgReplyID = int64(rawID)
				}
			}
			mu.Unlock()
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, napCatGetMsgData{
					MessageID:   88,
					MessageType: "private",
					UserID:      10001,
					Message:     mustJSONRawMessage(t, "被引用的内容"),
					Sender: struct {
						UserID   int64  `json:"user_id"`
						Nickname string `json:"nickname"`
						Card     string `json:"card"`
					}{
						UserID:   10001,
						Nickname: "Alice",
					},
				}),
			}
		case "send_private_msg":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, map[string]int64{"message_id": 999}),
			}
		default:
			return napCatActionResponse{}
		}
	})

	event := napCatMessageEvent{
		MessageID: 123,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "reply",
				Data: napCatMessageSegmentData{ID: "88"},
			},
			{
				Type: "text",
				Data: napCatMessageSegmentData{Text: "你好"},
			},
		}),
	}

	parsed, ok := service.parseIncomingPrivateMessage(source, event)
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	service.processIncomingBatch(context.Background(), config.NapCatConfig{}, source, []napCatPendingMessage{*parsed})

	mu.Lock()
	if getMsgCalls != 1 {
		t.Fatalf("get_msg calls = %d, want 1", getMsgCalls)
	}
	if getMsgReplyID != 88 {
		t.Fatalf("get_msg reply id = %d, want 88", getMsgReplyID)
	}
	mu.Unlock()

	sessions := handler.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	record, ok := handler.chatManager.GetSession(sessions[0].ID)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) == 0 {
		t.Fatal("expected persisted user message")
	}

	msg := record.Messages[0]
	if msg.Role != "user" {
		t.Fatalf("role = %q, want user", msg.Role)
	}
	if msg.Content != "你好" {
		t.Fatalf("content = %q, want %q", msg.Content, "你好")
	}
	if msg.Quote == nil {
		t.Fatal("expected quote to be persisted")
	}
	if msg.Quote.MessageID != "88" {
		t.Fatalf("quote.message_id = %q, want %q", msg.Quote.MessageID, "88")
	}
	if msg.Quote.MessageType != "private" {
		t.Fatalf("quote.message_type = %q, want %q", msg.Quote.MessageType, "private")
	}
	if msg.Quote.SenderUserID != "10001" {
		t.Fatalf("quote.sender_user_id = %q, want %q", msg.Quote.SenderUserID, "10001")
	}
	if msg.Quote.SenderNickname != "Alice" {
		t.Fatalf("quote.sender_nickname = %q, want %q", msg.Quote.SenderNickname, "Alice")
	}
	if msg.Quote.Content != "被引用的内容" {
		t.Fatalf("quote.content = %q, want %q", msg.Quote.Content, "被引用的内容")
	}
}

func TestNapCatProcessIncomingBatch_PersistsReplyQuoteImages(t *testing.T) {
	handler := newNapCatQuoteTestHandler(t)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	quoteImagePath := writeTinyPNGForNapCatTest(t)

	service := newNapCatTestService(t, handler, func(req napCatActionRequest) napCatActionResponse {
		switch req.Action {
		case "get_login_info":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, napCatLoginInfo{UserID: 20002, Nickname: "CornerStone"}),
			}
		case "get_msg":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, napCatGetMsgData{
					MessageID:   88,
					MessageType: "private",
					UserID:      10001,
					Message: mustJSONRawMessage(t, []napCatMessageSegment{
						{
							Type: "image",
							Data: napCatMessageSegmentData{Path: quoteImagePath},
						},
					}),
					Sender: struct {
						UserID   int64  `json:"user_id"`
						Nickname string `json:"nickname"`
						Card     string `json:"card"`
					}{
						UserID:   10001,
						Nickname: "Alice",
					},
				}),
			}
		case "send_private_msg":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, map[string]int64{"message_id": 999}),
			}
		default:
			return napCatActionResponse{}
		}
	})

	event := napCatMessageEvent{
		MessageID: 124,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "reply",
				Data: napCatMessageSegmentData{ID: "88"},
			},
			{
				Type: "text",
				Data: napCatMessageSegmentData{Text: "看看这个"},
			},
		}),
	}

	parsed, ok := service.parseIncomingPrivateMessage(source, event)
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	service.processIncomingBatch(context.Background(), config.NapCatConfig{}, source, []napCatPendingMessage{*parsed})

	sessions := handler.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	record, ok := handler.chatManager.GetSession(sessions[0].ID)
	if !ok || len(record.Messages) == 0 {
		t.Fatal("expected persisted session message")
	}
	msg := record.Messages[0]
	if msg.Quote == nil {
		t.Fatal("expected quote to be persisted")
	}
	if msg.Quote.Content != "[用户发送了图片]" {
		t.Fatalf("quote.content = %q, want image placeholder", msg.Quote.Content)
	}
	if len(msg.Quote.ImagePaths) != 1 {
		t.Fatalf("quote.image_paths len = %d, want 1", len(msg.Quote.ImagePaths))
	}
	if !strings.HasPrefix(msg.Quote.ImagePaths[0], cachePhotoDirName+"/") {
		t.Fatalf("quote.image_paths[0] = %q, want cache photo path", msg.Quote.ImagePaths[0])
	}

	converted := convertChatMessages([]storage.ChatMessage{msg})
	if len(converted) != 1 {
		t.Fatalf("converted len = %d, want 1", len(converted))
	}
	if len(converted[0].ImagePaths) != 1 {
		t.Fatalf("converted image_paths len = %d, want 1", len(converted[0].ImagePaths))
	}
	if !strings.Contains(converted[0].Content, "前 1 张图片来自引用消息") {
		t.Fatalf("converted content = %q, want quoted image notice", converted[0].Content)
	}
	if !strings.Contains(converted[0].Content, "[当前消息]\n看看这个") {
		t.Fatalf("converted content = %q, want current message section", converted[0].Content)
	}
}

func TestNapCatProcessIncomingBatch_GetMsgFailureUsesPlaceholderQuote(t *testing.T) {
	handler := newNapCatQuoteTestHandler(t)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}

	service := newNapCatTestService(t, handler, func(req napCatActionRequest) napCatActionResponse {
		switch req.Action {
		case "get_login_info":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, napCatLoginInfo{UserID: 20002, Nickname: "CornerStone"}),
			}
		case "get_msg":
			return napCatActionResponse{
				Status:  "failed",
				RetCode: 1,
				Message: "not found",
			}
		case "send_private_msg":
			return napCatActionResponse{
				Data: mustJSONRawMessage(t, map[string]int64{"message_id": 999}),
			}
		default:
			return napCatActionResponse{}
		}
	})

	event := napCatMessageEvent{
		MessageID: 125,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "reply",
				Data: napCatMessageSegmentData{ID: "88"},
			},
			{
				Type: "text",
				Data: napCatMessageSegmentData{Text: "继续说"},
			},
		}),
	}

	parsed, ok := service.parseIncomingPrivateMessage(source, event)
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	service.processIncomingBatch(context.Background(), config.NapCatConfig{}, source, []napCatPendingMessage{*parsed})

	sessions := handler.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}

	record, ok := handler.chatManager.GetSession(sessions[0].ID)
	if !ok || len(record.Messages) == 0 {
		t.Fatal("expected persisted session message")
	}
	msg := record.Messages[0]
	if msg.Quote == nil {
		t.Fatal("expected placeholder quote")
	}
	if msg.Quote.MessageID != "88" {
		t.Fatalf("quote.message_id = %q, want %q", msg.Quote.MessageID, "88")
	}
	if msg.Quote.Content != "[引用消息内容不可用]" {
		t.Fatalf("quote.content = %q, want placeholder", msg.Quote.Content)
	}
}

func TestNapCatParseIncomingPrivateMessage_FaceAndText(t *testing.T) {
	service := &NapCatService{}
	parsed, ok := service.parseIncomingPrivateMessage(napCatChatSource{
		Kind:   "private",
		SelfID: 20002,
		UserID: 10001,
	}, napCatMessageEvent{
		MessageID: 126,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "text",
				Data: napCatMessageSegmentData{Text: "你好"},
			},
			{
				Type: "face",
				Data: napCatMessageSegmentData{ID: "123"},
			},
		}),
	})
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	if parsed.Text != "你好[QQ表情#123]" {
		t.Fatalf("text = %q, want %q", parsed.Text, "你好[QQ表情#123]")
	}
	if len(parsed.ImageSegments) != 0 {
		t.Fatalf("image_segments len = %d, want 0", len(parsed.ImageSegments))
	}
}

func TestNapCatParseIncomingPrivateMessage_MFaceUsesSummary(t *testing.T) {
	service := &NapCatService{}
	parsed, ok := service.parseIncomingPrivateMessage(napCatChatSource{
		Kind:   "private",
		SelfID: 20002,
		UserID: 10001,
	}, napCatMessageEvent{
		MessageID: 127,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "mface",
				Data: napCatMessageSegmentData{Summary: "[摇头]"},
			},
		}),
	})
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	if parsed.Text != "[摇头]" {
		t.Fatalf("text = %q, want %q", parsed.Text, "[摇头]")
	}
}

func TestNapCatParseIncomingPrivateMessage_PokeUsesPlaceholder(t *testing.T) {
	service := &NapCatService{}
	parsed, ok := service.parseIncomingPrivateMessage(napCatChatSource{
		Kind:   "private",
		SelfID: 20002,
		UserID: 10001,
	}, napCatMessageEvent{
		MessageID: 128,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "poke",
				Data: napCatMessageSegmentData{PokeType: "126", ID: "1"},
			},
		}),
	})
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	if parsed.Text != "[戳一戳]" {
		t.Fatalf("text = %q, want %q", parsed.Text, "[戳一戳]")
	}
	if len(parsed.ImageSegments) != 0 {
		t.Fatalf("image_segments len = %d, want 0", len(parsed.ImageSegments))
	}
}

func TestNapCatHandleEvent_PokeEntersReplyWaitWindow(t *testing.T) {
	provider := newTestProvider("provider-1")
	configManager := newTestProviderConfigManager(t, provider)

	cfg := configManager.Get()
	cfg.ReplyWaitWindowMode = string(config.ReplyWaitWindowModeSliding)
	cfg.ReplyWaitWindowSeconds = 5
	cfg.NapCat.Enabled = true
	cfg.NapCat.AllowPrivate = true
	cfg.NapCat.AccessToken = "napcat-token"
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	service := &NapCatService{
		handler:              &Handler{configManager: configManager},
		pendingActionWaiters: make(map[string]chan *napCatActionResponse),
		activeSessions:       make(map[napCatChatSource]*napCatActiveSession),
		pendingReplies:       make(map[napCatChatSource]*napCatPendingReply),
	}

	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	service.handleEvent(context.Background(), napCatMessageEvent{
		PostType:    "message",
		MessageType: "private",
		MessageID:   129,
		SelfID:      source.SelfID,
		UserID:      source.UserID,
		Message: mustJSONRawMessage(t, []napCatMessageSegment{
			{
				Type: "poke",
				Data: napCatMessageSegmentData{PokeType: "126", ID: "1"},
			},
		}),
	})

	service.mu.Lock()
	state := service.pendingReplies[source]
	if state == nil {
		service.mu.Unlock()
		t.Fatal("expected poke event to enter pending reply window")
	}
	if len(state.Messages) != 1 {
		service.mu.Unlock()
		t.Fatalf("pending messages len = %d, want 1", len(state.Messages))
	}
	if state.Messages[0].Text != "[戳一戳]" {
		service.mu.Unlock()
		t.Fatalf("pending text = %q, want %q", state.Messages[0].Text, "[戳一戳]")
	}
	if state.Timer == nil {
		service.mu.Unlock()
		t.Fatal("expected reply wait window timer to be scheduled")
	}
	state.Timer.Stop()
	service.mu.Unlock()
}

func TestNapCatParseIncomingPrivateMessage_CQStringFallback(t *testing.T) {
	service := &NapCatService{}
	rawCQ := "[CQ:reply,id=88]看这个[CQ:face,id=123][CQ:image,file=/tmp/test.png]"

	parsed, ok := service.parseIncomingPrivateMessage(napCatChatSource{
		Kind:   "private",
		SelfID: 20002,
		UserID: 10001,
	}, napCatMessageEvent{
		MessageID:  128,
		Message:    mustJSONRawMessage(t, rawCQ),
		RawMessage: rawCQ,
	})
	if !ok || parsed == nil {
		t.Fatal("parseIncomingPrivateMessage returned no message")
	}
	if parsed.ReplyMessageID != 88 {
		t.Fatalf("reply_message_id = %d, want 88", parsed.ReplyMessageID)
	}
	if parsed.Text != "看这个[QQ表情#123]" {
		t.Fatalf("text = %q, want %q", parsed.Text, "看这个[QQ表情#123]")
	}
	if len(parsed.ImageSegments) != 1 {
		t.Fatalf("image_segments len = %d, want 1", len(parsed.ImageSegments))
	}
	if parsed.ImageSegments[0].Data.File != "/tmp/test.png" {
		t.Fatalf("image file = %q, want %q", parsed.ImageSegments[0].Data.File, "/tmp/test.png")
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

func TestReminderService_FireReminder_NapCatRetryableSendRequeuesPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
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
	napCfg := configManager.GetNapCatConfig()
	napCfg.Enabled = true
	napCfg.AllowPrivate = true
	napCfg.AccessToken = "napcat-token"
	if err := configManager.UpdateNapCatConfig(napCfg); err != nil {
		t.Fatalf("UpdateNapCatConfig failed: %v", err)
	}

	promptManager := newTestPromptManager(t)
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

	napCatSvc := NewNapCatService(handler)
	defer napCatSvc.Close()
	handler.SetNapCatService(napCatSvc)

	reminderSvc := NewReminderService(handler, storage.NewReminderManager(filepath.Join(t.TempDir(), "reminders")), exactTimeSvc)
	handler.SetReminderService(reminderSvc)

	reminder, err := reminderSvc.Create(reminderCreateRequest{
		Channel:    storage.ReminderChannelNapCat,
		SessionID:  "session-1",
		PromptID:   "prompt-1",
		PromptName: "Alice",
		Target: storage.ReminderTarget{
			Kind:      storage.ReminderTargetKindUser,
			UserID:    "10001",
			BotSelfID: "20002",
		},
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

	record, ok := chatMgr.GetSession("session-1")
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 1 {
		t.Fatalf("message count = %d, want 1 unchanged user message", len(record.Messages))
	}

	saved, ok := reminderSvc.Get(reminder.ID)
	if !ok || saved == nil {
		t.Fatal("saved reminder not found")
	}
	if saved.Status != storage.ReminderStatusPending {
		t.Fatalf("status = %q, want %q", saved.Status, storage.ReminderStatusPending)
	}
	if saved.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", saved.Attempts)
	}
	if !strings.Contains(saved.LastError, "napcat channel is unavailable") {
		t.Fatalf("last_error = %q, want temporary napcat channel error", saved.LastError)
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
