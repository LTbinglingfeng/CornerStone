package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/exacttime"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestIsClawBotNewCommand(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		match bool
	}{
		{name: "plain", text: "/new", match: true},
		{name: "trim spaces", text: "  /new  ", match: true},
		{name: "full width slash", text: "／new", match: true},
		{name: "trailing chinese punctuation", text: "/new。", match: true},
		{name: "newline suffix", text: "/new\n", match: true},
		{name: "with content", text: "/new hello", match: false},
		{name: "different word", text: "/newchat", match: false},
		{name: "embedded", text: "hello /new", match: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClawBotNewCommand(tc.text); got != tc.match {
				t.Fatalf("isClawBotNewCommand(%q) = %v, want %v", tc.text, got, tc.match)
			}
		})
	}
}

func TestParseClawBotCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		ok      bool
		command clawBotCommand
	}{
		{
			name:    "list command",
			text:    "/ls",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandList},
		},
		{
			name:    "checkout command with index",
			text:    "／checkout  2",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandCheckout, Args: "2"},
		},
		{
			name:    "new command with punctuation",
			text:    "/new。",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandNew},
		},
		{
			name:    "regenerate command",
			text:    " /re！ ",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandRegenerate},
		},
		{
			name:    "menu command",
			text:    "/menu",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandMenu},
		},
		{
			name:    "rename command",
			text:    "/rename 新标题",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandRename, Args: "新标题"},
		},
		{
			name:    "delete command",
			text:    "/delete current",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandDelete, Args: "current"},
		},
		{
			name:    "prompt command",
			text:    "/prompt ls",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandPrompt, Args: "ls"},
		},
		{
			name: "new command with content",
			text: "/new hello",
			ok:   true,
			command: clawBotCommand{
				Name: clawBotCommandNew,
				Args: "hello",
			},
		},
		{
			name: "not a command",
			text: "hello /ls",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, ok := parseClawBotCommand(tc.text)
			if ok != tc.ok {
				t.Fatalf("parseClawBotCommand(%q) ok = %v, want %v", tc.text, ok, tc.ok)
			}
			if !ok {
				return
			}
			if command != tc.command {
				t.Fatalf("parseClawBotCommand(%q) = %#v, want %#v", tc.text, command, tc.command)
			}
		})
	}
}

func TestResolveClawBotSessionSelector(t *testing.T) {
	sessions := []storage.ChatSession{
		{ID: "session-a"},
		{ID: "session-b"},
	}

	session, index, err := resolveClawBotSessionSelector(sessions, "2")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector index err = %v", err)
	}
	if index != 1 || session.ID != "session-b" {
		t.Fatalf("resolveClawBotSessionSelector index = (%d, %s), want (1, session-b)", index, session.ID)
	}

	session, index, err = resolveClawBotSessionSelector(sessions, "session-a")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector id err = %v", err)
	}
	if index != 0 || session.ID != "session-a" {
		t.Fatalf("resolveClawBotSessionSelector id = (%d, %s), want (0, session-a)", index, session.ID)
	}

	session, index, err = resolveClawBotSessionSelector(sessions, "b")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector suffix err = %v", err)
	}
	if index != 1 || session.ID != "session-b" {
		t.Fatalf("resolveClawBotSessionSelector suffix = (%d, %s), want (1, session-b)", index, session.ID)
	}

	if _, _, err := resolveClawBotSessionSelector(sessions, "3"); err == nil {
		t.Fatal("resolveClawBotSessionSelector out of range error = nil, want error")
	}
}

func TestResolveClawBotPromptSelector(t *testing.T) {
	prompts := []storage.Prompt{
		{ID: "prompt-1", Name: "Alice"},
		{ID: "prompt-2", Name: "Bob"},
	}

	prompt, index, useDefault, err := resolveClawBotPromptSelector(prompts, "2")
	if err != nil {
		t.Fatalf("resolveClawBotPromptSelector index err = %v", err)
	}
	if useDefault || index != 1 || prompt.ID != "prompt-2" {
		t.Fatalf("resolveClawBotPromptSelector index = (%v, %d, %s), want (false, 1, prompt-2)", useDefault, index, prompt.ID)
	}

	prompt, index, useDefault, err = resolveClawBotPromptSelector(prompts, "prompt-1")
	if err != nil {
		t.Fatalf("resolveClawBotPromptSelector id err = %v", err)
	}
	if useDefault || index != 0 || prompt.ID != "prompt-1" {
		t.Fatalf("resolveClawBotPromptSelector id = (%v, %d, %s), want (false, 0, prompt-1)", useDefault, index, prompt.ID)
	}

	prompt, index, useDefault, err = resolveClawBotPromptSelector(prompts, "Bob")
	if err != nil {
		t.Fatalf("resolveClawBotPromptSelector name err = %v", err)
	}
	if useDefault || index != 1 || prompt.ID != "prompt-2" {
		t.Fatalf("resolveClawBotPromptSelector name = (%v, %d, %s), want (false, 1, prompt-2)", useDefault, index, prompt.ID)
	}

	_, index, useDefault, err = resolveClawBotPromptSelector(prompts, "default")
	if err != nil {
		t.Fatalf("resolveClawBotPromptSelector default err = %v", err)
	}
	if !useDefault || index != -1 {
		t.Fatalf("resolveClawBotPromptSelector default = (%v, %d), want (true, -1)", useDefault, index)
	}

	if _, _, _, err := resolveClawBotPromptSelector(prompts, "3"); err == nil {
		t.Fatal("resolveClawBotPromptSelector out of range error = nil, want error")
	}
}

func TestSplitClawBotReplyMessages(t *testing.T) {
	t.Run("split by assistant token", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("第一句→第二句→第三句", 2000)
		want := []string{"第一句", "第二句", "第三句"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("split token segments still obey max runes", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("12345→67890", 3)
		want := []string{"123", "45", "678", "90"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("fallback without split token", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("abcdef", 4)
		want := []string{"abcd", "ef"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})
}

func TestGetOrCreateActiveSessionRespectsPrompt(t *testing.T) {
	chatManager := storage.NewChatManager(t.TempDir())
	service := &ClawBotService{
		handler: &Handler{
			chatManager: chatManager,
		},
		activeSessions: make(map[string]*clawBotActiveSession),
	}

	userID := "wx-user"
	promptA := "prompt-a"
	promptB := "prompt-b"

	sessionA, err := chatManager.CreateSession(generateClawBotSessionID(userID), "Session A", promptA, "Prompt A")
	if err != nil {
		t.Fatalf("create session A err = %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	sessionB, err := chatManager.CreateSession(generateClawBotSessionID(userID), "Session B", promptB, "Prompt B")
	if err != nil {
		t.Fatalf("create session B err = %v", err)
	}

	service.touchActiveSession(userID, sessionB.SessionID)

	active, err := service.getOrCreateActiveSession(userID, promptA)
	if err != nil {
		t.Fatalf("getOrCreateActiveSession err = %v", err)
	}
	if active.SessionID != sessionA.SessionID {
		t.Fatalf("getOrCreateActiveSession session = %s, want %s", active.SessionID, sessionA.SessionID)
	}

	if currentID, ok := service.getActiveSessionID(userID); !ok || currentID != sessionA.SessionID {
		t.Fatalf("active session id = (%v, %s), want (true, %s)", ok, currentID, sessionA.SessionID)
	}
}

type clawBotTestServerState struct {
	mu sync.Mutex

	chatRequests []struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
	}
	sendTexts []string
	sendUsers []string
}

func newTestClawBotService(t *testing.T, baseURL string) *ClawBotService {
	t.Helper()

	provider := newTestProvider("provider-1")
	provider.BaseURL = baseURL
	provider.Stream = false

	configManager := newTestProviderConfigManager(t, provider)
	cfg := configManager.Get()
	cfg.ClawBot = config.ClawBotConfig{
		Enabled:  true,
		BaseURL:  baseURL,
		BotToken: "bot-token",
	}
	cfg.ReplyWaitWindowSeconds = 0
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update clawbot config failed: %v", err)
	}

	handler := &Handler{
		configManager:  configManager,
		promptManager:  newTestPromptManager(t),
		chatManager:    storage.NewChatManager(t.TempDir()),
		userManager:    newTestUserManager(t),
		memoryManager:  storage.NewMemoryManager(t.TempDir()),
		memorySessions: make(map[string]*storage.MemorySession),
		cleanupDone:    make(chan struct{}),
	}
	handler.SetReminderService(NewReminderService(handler, storage.NewReminderManager(filepath.Join(t.TempDir(), "reminders")), nil))

	return &ClawBotService{
		handler:        handler,
		client:         client.NewClawBotClient(),
		activeSessions: make(map[string]*clawBotActiveSession),
		pendingReplies: make(map[string]*clawBotPendingReply),
		contextTokens:  make(map[string]string),
		wechatUIN:      "wechat-uin",
	}
}

func newClawBotTestServer(t *testing.T, replyText string) (*httptest.Server, *clawBotTestServerState) {
	t.Helper()

	state := &clawBotTestServerState{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Role    string      `json:"role"`
					Content interface{} `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatRequests = append(state.chatRequests, req)
			state.mu.Unlock()

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
							Content: replyText,
						},
						FinishReason: "stop",
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.sendUsers = append(state.sendUsers, req.Msg.ToUserID)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))

	return server, state
}

func newClawBotTextIncomingMessage(userID, text string) client.ClawBotIncomingMessage {
	return client.ClawBotIncomingMessage{
		MessageType: 1,
		FromUserID:  userID,
		ItemList: []client.ClawBotIncomingMessageItem{
			{
				Type: 1,
				TextItem: &client.ClawBotItemText{
					Text: text,
				},
			},
		},
	}
}

func waitForAssistantMessageContent(t *testing.T, chatManager *storage.ChatManager, sessionID, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		record, ok := chatManager.GetSession(sessionID)
		if ok && len(record.Messages) >= 2 {
			last := record.Messages[len(record.Messages)-1]
			if last.Role == "assistant" && last.Content == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	record, _ := chatManager.GetSession(sessionID)
	t.Fatalf("assistant message did not become %q in time, got %#v", want, record.Messages)
}

func waitForClawBotChatRequestCount(t *testing.T, state *clawBotTestServerState, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		got := len(state.chatRequests)
		state.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	state.mu.Lock()
	got := len(state.chatRequests)
	state.mu.Unlock()
	t.Fatalf("chat request count = %d, want %d", got, want)
}

func TestHandleMenuCommand_IncludesNewCommands(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	service.handleMenuCommand(context.Background(), cfg, "wx-user", "")

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	reply := state.sendTexts[0]
	for _, want := range []string{"/menu", "/rename <标题>", "/delete <序号|current>", "/prompt ls"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("menu reply = %q, want substring %q", reply, want)
		}
	}
}

func TestHandleRenameCommand_UpdatesCurrentSessionTitle(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Old Title", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	service.touchActiveSession(userID, session.SessionID)

	service.handleRenameCommand(context.Background(), cfg, userID, "New Title")

	state.mu.Lock()
	if len(state.sendTexts) != 1 {
		state.mu.Unlock()
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "New Title") {
		text := state.sendTexts[0]
		state.mu.Unlock()
		t.Fatalf("rename reply = %q, want New Title", text)
	}
	state.mu.Unlock()

	record, ok := service.handler.chatManager.GetSession(session.SessionID)
	if !ok {
		t.Fatal("session not found after rename")
	}
	if record.Title != "New Title" {
		t.Fatalf("session title = %q, want %q", record.Title, "New Title")
	}
}

func TestHandleRenameCommand_TruncatesLongTitleAndEchoesStoredTitle(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Old Title", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	service.touchActiveSession(userID, session.SessionID)

	longTitle := strings.Repeat("你", 130)
	service.handleRenameCommand(context.Background(), cfg, userID, longTitle)

	wantTitle := strings.Repeat("你", 117) + "..."

	state.mu.Lock()
	if len(state.sendTexts) != 1 {
		state.mu.Unlock()
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	reply := state.sendTexts[0]
	state.mu.Unlock()
	if !strings.Contains(reply, wantTitle) {
		t.Fatalf("rename reply = %q, want truncated title %q", reply, wantTitle)
	}

	record, ok := service.handler.chatManager.GetSession(session.SessionID)
	if !ok {
		t.Fatal("session not found after rename")
	}
	if record.Title != wantTitle {
		t.Fatalf("session title = %q, want %q", record.Title, wantTitle)
	}
	if utf8.RuneCountInString(record.Title) != 120 {
		t.Fatalf("session title runes = %d, want 120", utf8.RuneCountInString(record.Title))
	}
}

func TestHandleDeleteCommand_DeletesCurrentSessionAndSwitchesToRemainingSession(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionA, err := service.handler.chatManager.CreateSession(generateClawBotSessionID(userID), "Session A", "", "")
	if err != nil {
		t.Fatalf("CreateSession A err = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	sessionB, err := service.handler.chatManager.CreateSession(generateClawBotSessionID(userID), "Session B", "", "")
	if err != nil {
		t.Fatalf("CreateSession B err = %v", err)
	}
	service.touchActiveSession(userID, sessionB.SessionID)

	service.handleDeleteCommand(context.Background(), cfg, userID, "current")

	state.mu.Lock()
	if len(state.sendTexts) != 1 {
		state.mu.Unlock()
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "已删除会话") || !strings.Contains(state.sendTexts[0], "Session A") {
		text := state.sendTexts[0]
		state.mu.Unlock()
		t.Fatalf("delete reply = %q, want delete confirmation and Session A switch hint", text)
	}
	state.mu.Unlock()

	if _, ok := service.handler.chatManager.GetSession(sessionB.SessionID); ok {
		t.Fatal("deleted session still exists")
	}
	if currentID, ok := service.getActiveSessionID(userID); !ok || currentID != sessionA.SessionID {
		t.Fatalf("active session id = (%v, %s), want (true, %s)", ok, currentID, sessionA.SessionID)
	}
}

func TestHandlePromptCommand_SwitchesPromptAndActivatesLatestPromptSession(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	if _, err := service.handler.promptManager.Create("prompt-2", "Bob", "prompt two", "", ""); err != nil {
		t.Fatalf("Create prompt-2 err = %v", err)
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	cfg.PromptID = "prompt-1"
	if err := service.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatalf("UpdateClawBotConfig err = %v", err)
	}

	userID := "wx-user"
	session, err := service.handler.chatManager.CreateSession(generateClawBotSessionID(userID), "Bob Session", "prompt-2", "Bob")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}

	service.handlePromptCommand(context.Background(), cfg, userID, "prompt-2")

	state.mu.Lock()
	if len(state.sendTexts) != 1 {
		state.mu.Unlock()
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "Bob") || !strings.Contains(state.sendTexts[0], "Bob Session") {
		text := state.sendTexts[0]
		state.mu.Unlock()
		t.Fatalf("prompt reply = %q, want Bob prompt switch confirmation", text)
	}
	state.mu.Unlock()

	updatedCfg := service.handler.configManager.GetClawBotConfig()
	if updatedCfg.PromptID != "prompt-2" {
		t.Fatalf("prompt id = %q, want %q", updatedCfg.PromptID, "prompt-2")
	}
	if currentID, ok := service.getActiveSessionID(userID); !ok || currentID != session.SessionID {
		t.Fatalf("active session id = (%v, %s), want (true, %s)", ok, currentID, session.SessionID)
	}
}

func TestHandleIncomingMessage_UnknownCommandDoesNotReachModel(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage("wx-user", "/men"))

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatRequests) != 0 {
		t.Fatalf("chat request count = %d, want 0", len(state.chatRequests))
	}
	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	reply := state.sendTexts[0]
	if !strings.Contains(reply, "未知命令") || !strings.Contains(reply, "/menu") || !strings.Contains(reply, "//menu") {
		t.Fatalf("unknown command reply = %q, want unknown-command guidance", reply)
	}
	if sessions := service.handler.chatManager.ListSessions(); len(sessions) != 0 {
		t.Fatalf("session count = %d, want 0", len(sessions))
	}
}

func TestHandleIncomingMessage_KnownCommandWithInvalidArgsReturnsUsage(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage("wx-user", "/new hello"))

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatRequests) != 0 {
		t.Fatalf("chat request count = %d, want 0", len(state.chatRequests))
	}
	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "用法：/new") {
		t.Fatalf("usage reply = %q, want /new usage", state.sendTexts[0])
	}
}

func TestHandleIncomingMessage_DisabledCommandReturnsHint(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()
	cfg.CommandPermissions = config.NormalizeClawBotCommandPermissions(map[string]bool{
		clawBotCommandNew:        true,
		clawBotCommandList:       true,
		clawBotCommandCheckout:   true,
		clawBotCommandRename:     false,
		clawBotCommandDelete:     true,
		clawBotCommandPrompt:     true,
		clawBotCommandRegenerate: true,
	})
	if err := service.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatalf("UpdateClawBotConfig err = %v", err)
	}

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage("wx-user", "/rename test"))

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatRequests) != 0 {
		t.Fatalf("chat request count = %d, want 0", len(state.chatRequests))
	}
	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "命令已禁用") || !strings.Contains(state.sendTexts[0], "/rename") {
		t.Fatalf("disabled command reply = %q, want disabled hint", state.sendTexts[0])
	}
}

func TestHandleIncomingMessage_DoubleSlashEscapesCommandText(t *testing.T) {
	server, state := newClawBotTestServer(t, "assistant reply")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage("wx-user", "//menu"))

	waitForClawBotChatRequestCount(t, state, 1)

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 || state.sendTexts[0] != "assistant reply" {
		t.Fatalf("send texts = %#v, want assistant reply", state.sendTexts)
	}
	lastMessage := state.chatRequests[0].Messages[len(state.chatRequests[0].Messages)-1]
	content, _ := lastMessage.Content.(string)
	if lastMessage.Role != "user" || content != "/menu" {
		t.Fatalf("last request message = (%s, %v), want user /menu", lastMessage.Role, lastMessage.Content)
	}
}

func TestHandleMenuCommand_OmitsDisabledCommands(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()
	cfg.CommandPermissions = config.NormalizeClawBotCommandPermissions(map[string]bool{
		clawBotCommandNew:        false,
		clawBotCommandList:       true,
		clawBotCommandCheckout:   true,
		clawBotCommandRename:     false,
		clawBotCommandDelete:     false,
		clawBotCommandPrompt:     true,
		clawBotCommandRegenerate: true,
	})

	service.handleMenuCommand(context.Background(), cfg, "wx-user", "")

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	reply := state.sendTexts[0]
	if strings.Contains(reply, "/new 开始新聊天") || strings.Contains(reply, "/rename <标题>") || strings.Contains(reply, "/delete <序号|current>") {
		t.Fatalf("menu reply = %q, want disabled commands omitted", reply)
	}
	if !strings.Contains(reply, "/ls 查看当前人设下的会话列表") || !strings.Contains(reply, "/menu 查看此菜单") {
		t.Fatalf("menu reply = %q, want enabled commands retained", reply)
	}
}

func TestHandlePromptCommand_AmbiguousPromptSelectorReturnsHint(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	if _, err := service.handler.promptManager.Create("prompt-2", "Alex", "prompt two", "", ""); err != nil {
		t.Fatalf("Create prompt-2 err = %v", err)
	}
	if _, err := service.handler.promptManager.Create("prompt-3", "Alex", "prompt three", "", ""); err != nil {
		t.Fatalf("Create prompt-3 err = %v", err)
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	service.handlePromptCommand(context.Background(), cfg, "wx-user", "Alex")

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "匹配到多个同名人设") {
		t.Fatalf("prompt ambiguous reply = %q, want ambiguous hint", state.sendTexts[0])
	}
}

func TestHandleCheckoutCommand_AmbiguousSessionSelectorReturnsHint(t *testing.T) {
	server, state := newClawBotTestServer(t, "unused")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	prefix := clawBotSessionPrefix(userID)
	if _, err := service.handler.chatManager.CreateSession(prefix+"alpha_sharedtail", "Session Alpha", "", ""); err != nil {
		t.Fatalf("CreateSession alpha err = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := service.handler.chatManager.CreateSession(prefix+"beta_sharedtail", "Session Beta", "", ""); err != nil {
		t.Fatalf("CreateSession beta err = %v", err)
	}

	service.handleCheckoutCommand(context.Background(), cfg, userID, "sharedtail")

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "匹配到多个会话") {
		t.Fatalf("checkout ambiguous reply = %q, want ambiguous hint", state.sendTexts[0])
	}
}

func TestProcessRegenerateCommand_ReplacesTailAndSendsFreshReply(t *testing.T) {
	server, state := newClawBotTestServer(t, "fresh reply")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Session", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}

	now := time.Now()
	err = service.handler.chatManager.AddMessages(session.SessionID, []storage.ChatMessage{
		{Role: "user", Content: "hello", Timestamp: now},
		{Role: "assistant", Content: "old reply", Timestamp: now.Add(time.Millisecond)},
		{Role: "tool", Content: `{"ok":true}`, ToolCallID: "call-1", Timestamp: now.Add(2 * time.Millisecond)},
	})
	if err != nil {
		t.Fatalf("AddMessages err = %v", err)
	}

	service.touchActiveSession(userID, session.SessionID)
	service.setContextToken(userID, "ctx-token")

	service.processRegenerateCommand(context.Background(), cfg, userID)

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatRequests) != 1 {
		t.Fatalf("chat request count = %d, want 1", len(state.chatRequests))
	}
	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if state.sendTexts[0] != "fresh reply" {
		t.Fatalf("sent text = %q, want %q", state.sendTexts[0], "fresh reply")
	}

	chatReq := state.chatRequests[0]
	if len(chatReq.Messages) < 2 {
		t.Fatalf("chat request messages len = %d, want at least 2", len(chatReq.Messages))
	}
	lastMessage := chatReq.Messages[len(chatReq.Messages)-1]
	if lastMessage.Role != "user" || lastMessage.Content != "hello" {
		t.Fatalf("last request message = (%s, %v), want user hello", lastMessage.Role, lastMessage.Content)
	}
	for _, msg := range chatReq.Messages {
		content, _ := msg.Content.(string)
		if strings.Contains(content, "old reply") {
			t.Fatalf("old reply should have been removed from regenerate request, got messages=%#v", chatReq.Messages)
		}
	}

	record, ok := service.handler.chatManager.GetSession(session.SessionID)
	if !ok {
		t.Fatal("session not found after regenerate")
	}
	if len(record.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(record.Messages))
	}
	if record.Messages[0].Role != "user" || record.Messages[0].Content != "hello" {
		t.Fatalf("first message = %#v, want user hello", record.Messages[0])
	}
	if record.Messages[1].Role != "assistant" || record.Messages[1].Content != "fresh reply" {
		t.Fatalf("second message = %#v, want assistant fresh reply", record.Messages[1])
	}
}

func TestProcessIncomingBatch_ClawBotRunsReadOnlyToolLoop(t *testing.T) {
	var state struct {
		mu       sync.Mutex
		chatReqs []struct {
			Messages []struct {
				Role       string            `json:"role"`
				Content    interface{}       `json:"content"`
				ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
				ToolCallID string            `json:"tool_call_id,omitempty"`
			} `json:"messages"`
			Tools []client.Tool `json:"tools"`
		}
		sendTexts []string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Role       string            `json:"role"`
					Content    interface{}       `json:"content"`
					ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
					ToolCallID string            `json:"tool_call_id,omitempty"`
				} `json:"messages"`
				Tools []client.Tool `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatReqs = append(state.chatReqs, req)
			callIndex := len(state.chatReqs)
			state.mu.Unlock()

			respMessage := client.Message{
				Role:    "assistant",
				Content: "现在是 2026-04-04 18:30:45。",
			}
			finishReason := "stop"
			if callIndex == 1 {
				respMessage = client.Message{
					Role: "assistant",
					ToolCalls: []client.ToolCall{
						{
							ID:   "call_time_1",
							Type: "function",
							Function: client.ToolCallFunction{
								Name:      "get_time",
								Arguments: `{}`,
							},
						},
					},
				}
				finishReason = "tool_calls"
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ChatResponse{
				ID:      "chatcmpl-test",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-test",
				Choices: []client.Choice{
					{
						Index:        0,
						Message:      respMessage,
						FinishReason: finishReason,
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	service.handler.exactTimeService = &stubExactTimeService{
		now: time.Date(2026, 4, 4, 10, 30, 45, 0, time.UTC),
		status: exacttime.Status{
			Server:      "ntp.aliyun.com",
			LastSuccess: true,
			Message:     "ntp sync succeeded",
		},
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	service.processIncomingBatch(context.Background(), cfg, "wx-user", []clawBotPendingMessage{{Text: "现在几点"}})

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatReqs) != 2 {
		t.Fatalf("chat request count = %d, want 2", len(state.chatReqs))
	}
	if len(state.sendTexts) != 1 || state.sendTexts[0] != "现在是 2026-04-04 18:30:45。" {
		t.Fatalf("send texts = %#v, want final assistant reply", state.sendTexts)
	}

	firstReq := state.chatReqs[0]
	if len(firstReq.Tools) != 4 {
		t.Fatalf("first request tools len = %d, want 4", len(firstReq.Tools))
	}
	for _, tool := range firstReq.Tools {
		switch tool.Function.Name {
		case "get_time", "get_weather", "schedule_reminder", "no_reply":
		default:
			t.Fatalf("unexpected clawbot tool %q", tool.Function.Name)
		}
	}

	secondReq := state.chatReqs[1]
	foundToolResult := false
	for _, msg := range secondReq.Messages {
		content, _ := msg.Content.(string)
		if msg.Role == "tool" && msg.ToolCallID == "call_time_1" && strings.Contains(content, `"tool":"get_time"`) {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("second request missing get_time tool result: %#v", secondReq.Messages)
	}

	sessionID, ok := service.getActiveSessionID("wx-user")
	if !ok {
		t.Fatal("active session not found")
	}
	record, ok := service.handler.chatManager.GetSession(sessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(record.Messages))
	}
	if record.Messages[1].Role != "assistant" || len(record.Messages[1].ToolCalls) != 1 || record.Messages[1].ToolCalls[0].Function.Name != "get_time" {
		t.Fatalf("assistant tool call message = %#v, want get_time tool call", record.Messages[1])
	}
	if record.Messages[2].Role != "tool" || record.Messages[2].ToolCallID != "call_time_1" {
		t.Fatalf("tool message = %#v, want tool result for call_time_1", record.Messages[2])
	}
	if record.Messages[3].Role != "assistant" || record.Messages[3].Content != "现在是 2026-04-04 18:30:45。" {
		t.Fatalf("final assistant message = %#v, want final reply", record.Messages[3])
	}
}

func TestProcessIncomingBatch_ClawBotRunsWebSearchToolLoopWhenConfigured(t *testing.T) {
	var state struct {
		mu       sync.Mutex
		chatReqs []struct {
			Messages []struct {
				Role       string            `json:"role"`
				Content    interface{}       `json:"content"`
				ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
				ToolCallID string            `json:"tool_call_id,omitempty"`
			} `json:"messages"`
			Tools []client.Tool `json:"tools"`
		}
		sendTexts []string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Role       string            `json:"role"`
					Content    interface{}       `json:"content"`
					ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
					ToolCallID string            `json:"tool_call_id,omitempty"`
				} `json:"messages"`
				Tools []client.Tool `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatReqs = append(state.chatReqs, req)
			callIndex := len(state.chatReqs)
			state.mu.Unlock()

			respMessage := client.Message{
				Role:    "assistant",
				Content: "我查到 CornerStone 是一个多模型聊天应用。",
			}
			finishReason := "stop"
			if callIndex == 1 {
				respMessage = client.Message{
					Role: "assistant",
					ToolCalls: []client.ToolCall{
						{
							ID:   "call_search_1",
							Type: "function",
							Function: client.ToolCallFunction{
								Name:      "web_search",
								Arguments: `{"query":"CornerStone app 是什么"}`,
							},
						},
					},
				}
				finishReason = "tool_calls"
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ChatResponse{
				ID:      "chatcmpl-test",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-test",
				Choices: []client.Choice{
					{
						Index:        0,
						Message:      respMessage,
						FinishReason: finishReason,
					},
				},
			})
		case "/search":
			if auth := r.Header.Get("Authorization"); auth != "Bearer test-search-key" {
				t.Fatalf("Authorization = %q, want %q", auth, "Bearer test-search-key")
			}

			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode search request failed: %v", err)
			}
			if req["query"] != "CornerStone app 是什么" {
				t.Fatalf("search query = %v, want %q", req["query"], "CornerStone app 是什么")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"query":"CornerStone app 是什么","results":[{"title":"CornerStone","url":"https://example.com","content":"A chat app"}]}`))
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfgAll := service.handler.configManager.Get()
	cfgAll.WebSearch.ActiveProviderID = "tavily"
	cfgAll.WebSearch.MaxResults = 3
	cfgAll.WebSearch.FetchResults = 3
	cfgAll.WebSearch.TimeoutSeconds = 5
	cfgAll.WebSearch.Providers = map[string]config.WebSearchProvider{
		"tavily": {
			APIKey:  "test-search-key",
			APIHost: server.URL,
		},
	}
	if err := service.handler.configManager.Update(cfgAll); err != nil {
		t.Fatalf("Update web search config failed: %v", err)
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	service.processIncomingBatch(context.Background(), cfg, "wx-user", []clawBotPendingMessage{{Text: "帮我查一下 CornerStone 是什么"}})

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatReqs) != 2 {
		t.Fatalf("chat request count = %d, want 2", len(state.chatReqs))
	}
	if len(state.sendTexts) != 1 || state.sendTexts[0] != "我查到 CornerStone 是一个多模型聊天应用。" {
		t.Fatalf("send texts = %#v, want final assistant reply", state.sendTexts)
	}

	firstReq := state.chatReqs[0]
	if len(firstReq.Tools) != 5 {
		t.Fatalf("first request tools len = %d, want 5", len(firstReq.Tools))
	}
	names := make(map[string]struct{}, len(firstReq.Tools))
	for _, tool := range firstReq.Tools {
		names[tool.Function.Name] = struct{}{}
	}
	if _, ok := names["get_time"]; !ok {
		t.Fatalf("first request tools = %#v, want get_time", firstReq.Tools)
	}
	if _, ok := names["get_weather"]; !ok {
		t.Fatalf("first request tools = %#v, want get_weather", firstReq.Tools)
	}
	if _, ok := names["web_search"]; !ok {
		t.Fatalf("first request tools = %#v, want web_search", firstReq.Tools)
	}
	if _, ok := names["schedule_reminder"]; !ok {
		t.Fatalf("first request tools = %#v, want schedule_reminder", firstReq.Tools)
	}
	if _, ok := names["no_reply"]; !ok {
		t.Fatalf("first request tools = %#v, want no_reply", firstReq.Tools)
	}

	secondReq := state.chatReqs[1]
	foundToolResult := false
	for _, msg := range secondReq.Messages {
		content, _ := msg.Content.(string)
		if msg.Role == "tool" && msg.ToolCallID == "call_search_1" && strings.Contains(content, `"tool":"web_search"`) {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("second request missing web_search tool result: %#v", secondReq.Messages)
	}

	sessionID, ok := service.getActiveSessionID("wx-user")
	if !ok {
		t.Fatal("active session not found")
	}
	record, ok := service.handler.chatManager.GetSession(sessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(record.Messages))
	}
	if record.Messages[1].Role != "assistant" || len(record.Messages[1].ToolCalls) != 1 || record.Messages[1].ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("assistant tool call message = %#v, want web_search tool call", record.Messages[1])
	}
	if record.Messages[2].Role != "tool" || record.Messages[2].ToolCallID != "call_search_1" {
		t.Fatalf("tool message = %#v, want tool result for call_search_1", record.Messages[2])
	}
	if record.Messages[3].Role != "assistant" || record.Messages[3].Content != "我查到 CornerStone 是一个多模型聊天应用。" {
		t.Fatalf("final assistant message = %#v, want final reply", record.Messages[3])
	}
}

func TestProcessIncomingBatch_ClawBotRunsWriteMemoryToolLoopWhenEnabled(t *testing.T) {
	var state struct {
		mu       sync.Mutex
		chatReqs []struct {
			Messages []struct {
				Role       string            `json:"role"`
				Content    interface{}       `json:"content"`
				ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
				ToolCallID string            `json:"tool_call_id,omitempty"`
			} `json:"messages"`
			Tools []client.Tool `json:"tools"`
		}
		sendTexts []string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Role       string            `json:"role"`
					Content    interface{}       `json:"content"`
					ToolCalls  []client.ToolCall `json:"tool_calls,omitempty"`
					ToolCallID string            `json:"tool_call_id,omitempty"`
				} `json:"messages"`
				Tools []client.Tool `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatReqs = append(state.chatReqs, req)
			callIndex := len(state.chatReqs)
			state.mu.Unlock()

			respMessage := client.Message{
				Role:    "assistant",
				Content: "记住了。",
			}
			finishReason := "stop"
			if callIndex == 1 {
				respMessage = client.Message{
					Role: "assistant",
					ToolCalls: []client.ToolCall{
						{
							ID:   "call_memory_1",
							Type: "function",
							Function: client.ToolCallFunction{
								Name:      "write_memory",
								Arguments: `{"items":[{"subject":"user","category":"preference","content":"用户喜欢黑咖啡"}]}`,
							},
						},
					},
				}
				finishReason = "tool_calls"
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ChatResponse{
				ID:      "chatcmpl-test",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-test",
				Choices: []client.Choice{
					{
						Index:        0,
						Message:      respMessage,
						FinishReason: finishReason,
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfgAll := service.handler.configManager.Get()
	cfgAll.MemoryEnabled = true
	cfgAll.ClawBot.PromptID = "prompt-1"
	if err := service.handler.configManager.Update(cfgAll); err != nil {
		t.Fatalf("Update clawbot memory config failed: %v", err)
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	service.processIncomingBatch(context.Background(), cfg, "wx-user", []clawBotPendingMessage{{Text: "我喜欢黑咖啡"}})

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatReqs) != 2 {
		t.Fatalf("chat request count = %d, want 2", len(state.chatReqs))
	}
	if len(state.sendTexts) != 1 || state.sendTexts[0] != "记住了。" {
		t.Fatalf("send texts = %#v, want final assistant reply", state.sendTexts)
	}

	firstReq := state.chatReqs[0]
	if len(firstReq.Tools) != 5 {
		t.Fatalf("first request tools len = %d, want 5", len(firstReq.Tools))
	}
	names := make(map[string]struct{}, len(firstReq.Tools))
	for _, tool := range firstReq.Tools {
		names[tool.Function.Name] = struct{}{}
	}
	if _, ok := names["get_time"]; !ok {
		t.Fatalf("first request tools = %#v, want get_time", firstReq.Tools)
	}
	if _, ok := names["get_weather"]; !ok {
		t.Fatalf("first request tools = %#v, want get_weather", firstReq.Tools)
	}
	if _, ok := names["schedule_reminder"]; !ok {
		t.Fatalf("first request tools = %#v, want schedule_reminder", firstReq.Tools)
	}
	if _, ok := names["write_memory"]; !ok {
		t.Fatalf("first request tools = %#v, want write_memory", firstReq.Tools)
	}
	if _, ok := names["no_reply"]; !ok {
		t.Fatalf("first request tools = %#v, want no_reply", firstReq.Tools)
	}

	secondReq := state.chatReqs[1]
	foundToolResult := false
	for _, msg := range secondReq.Messages {
		content, _ := msg.Content.(string)
		if msg.Role == "tool" && msg.ToolCallID == "call_memory_1" && strings.Contains(content, `"tool":"write_memory"`) {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("second request missing write_memory tool result: %#v", secondReq.Messages)
	}

	memories := service.handler.memoryManager.GetAll("prompt-1")
	if len(memories) != 1 {
		t.Fatalf("memories len = %d, want 1", len(memories))
	}
	if memories[0].Content != "用户喜欢黑咖啡" {
		t.Fatalf("memory content = %q, want %q", memories[0].Content, "用户喜欢黑咖啡")
	}
}

func TestProcessIncomingBatch_ClawBotCanCreateScheduleReminder(t *testing.T) {
	var state struct {
		mu       sync.Mutex
		chatReqs []struct {
			Tools []client.Tool `json:"tools"`
		}
		sendTexts []string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Tools []client.Tool `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatReqs = append(state.chatReqs, req)
			callIndex := len(state.chatReqs)
			state.mu.Unlock()

			respMessage := client.Message{
				Role:    "assistant",
				Content: "好，到时我会提醒你。",
			}
			finishReason := "stop"
			if callIndex == 1 {
				respMessage = client.Message{
					Role: "assistant",
					ToolCalls: []client.ToolCall{
						{
							ID:   "call_reminder_1",
							Type: "function",
							Function: client.ToolCallFunction{
								Name:      "schedule_reminder",
								Arguments: `{"due_at":"2026-04-05T19:30:00+08:00","title":"喝水提醒","reminder_prompt":"到时间后提醒用户去喝水，并简短关心一下。"}`,
							},
						},
					},
				}
				finishReason = "tool_calls"
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ChatResponse{
				ID:      "chatcmpl-test",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "gpt-test",
				Choices: []client.Choice{
					{
						Index:        0,
						Message:      respMessage,
						FinishReason: finishReason,
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfgAll := service.handler.configManager.Get()
	cfgAll.ClawBot.PromptID = "prompt-1"
	if err := service.handler.configManager.Update(cfgAll); err != nil {
		t.Fatalf("Update clawbot prompt config failed: %v", err)
	}

	cfg := service.handler.configManager.GetClawBotConfig()
	service.processIncomingBatch(context.Background(), cfg, "wx-user", []clawBotPendingMessage{{Text: "今晚七点半提醒我喝水"}})

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.sendTexts) != 1 || state.sendTexts[0] != "好，到时我会提醒你。" {
		t.Fatalf("send texts = %#v, want final confirmation", state.sendTexts)
	}
	if len(state.chatReqs) == 0 {
		t.Fatal("expected at least one chat request")
	}

	foundReminderTool := false
	for _, tool := range state.chatReqs[0].Tools {
		if tool.Function.Name == "schedule_reminder" {
			foundReminderTool = true
			break
		}
	}
	if !foundReminderTool {
		t.Fatalf("first request tools = %#v, want schedule_reminder", state.chatReqs[0].Tools)
	}

	reminders := service.handler.reminderService.List()
	if len(reminders) != 1 {
		t.Fatalf("reminders len = %d, want 1", len(reminders))
	}
	if reminders[0].Channel != storage.ReminderChannelClawBot {
		t.Fatalf("reminder channel = %q, want %q", reminders[0].Channel, storage.ReminderChannelClawBot)
	}
	if reminders[0].Target.UserID != "wx-user" {
		t.Fatalf("target.user_id = %q, want %q", reminders[0].Target.UserID, "wx-user")
	}
	if reminders[0].PromptID != "prompt-1" {
		t.Fatalf("prompt_id = %q, want %q", reminders[0].PromptID, "prompt-1")
	}
	if reminders[0].Title != "喝水提醒" {
		t.Fatalf("title = %q, want %q", reminders[0].Title, "喝水提醒")
	}
}

func TestProcessIncomingBatch_ClawBotNoReplyPersistsSilently(t *testing.T) {
	var state struct {
		mu       sync.Mutex
		chatReqs []struct {
			Tools []client.Tool `json:"tools"`
		}
		sendTexts []string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var req struct {
				Tools []client.Tool `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			state.mu.Lock()
			state.chatReqs = append(state.chatReqs, req)
			state.mu.Unlock()

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
							Role: "assistant",
							ToolCalls: []client.ToolCall{
								{
									ID:   "call_no_reply_1",
									Type: "function",
									Function: client.ToolCallFunction{
										Name:      "no_reply",
										Arguments: `{"reason":"生气了","cooldown_seconds":120}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}

			state.mu.Lock()
			state.sendTexts = append(state.sendTexts, text)
			state.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()
	service.processIncomingBatch(context.Background(), cfg, "wx-user", []clawBotPendingMessage{{Text: "随便你"}})

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatReqs) != 1 {
		t.Fatalf("chat request count = %d, want 1", len(state.chatReqs))
	}
	if len(state.sendTexts) != 0 {
		t.Fatalf("send texts = %#v, want no outgoing messages", state.sendTexts)
	}
	foundNoReplyTool := false
	for _, tool := range state.chatReqs[0].Tools {
		if tool.Function.Name == "no_reply" {
			foundNoReplyTool = true
			break
		}
	}
	if !foundNoReplyTool {
		t.Fatalf("first request tools = %#v, want no_reply", state.chatReqs[0].Tools)
	}

	sessionID, ok := service.getActiveSessionID("wx-user")
	if !ok {
		t.Fatal("active session not found")
	}
	record, ok := service.handler.chatManager.GetSession(sessionID)
	if !ok {
		t.Fatal("session not found")
	}
	if len(record.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(record.Messages))
	}
	if record.Messages[1].Role != "assistant" || len(record.Messages[1].ToolCalls) != 1 || record.Messages[1].ToolCalls[0].Function.Name != "no_reply" {
		t.Fatalf("assistant tool call message = %#v, want no_reply tool call", record.Messages[1])
	}
	if record.Messages[2].Role != "tool" || record.Messages[2].ToolCallID != "call_no_reply_1" || !strings.Contains(record.Messages[2].Content, `"tool":"no_reply"`) {
		t.Fatalf("tool message = %#v, want no_reply tool result", record.Messages[2])
	}
	if record.Messages[3].Role != "assistant" || record.Messages[3].Content != "" || len(record.Messages[3].ToolCalls) != 0 {
		t.Fatalf("final assistant message = %#v, want silent assistant", record.Messages[3])
	}
}

func TestProcessRegenerateCommand_SendFailureKeepsOldAssistantReply(t *testing.T) {
	var (
		mu           sync.Mutex
		chatRequests int
		sendAttempts int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			mu.Lock()
			chatRequests++
			mu.Unlock()
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
							Content: "fresh reply",
						},
						FinishReason: "stop",
					},
				},
			})
		case "/ilink/bot/sendmessage":
			mu.Lock()
			sendAttempts++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Session", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	if err := service.handler.chatManager.AddMessages(session.SessionID, []storage.ChatMessage{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
		{Role: "assistant", Content: "old reply", Timestamp: time.Now().Add(time.Millisecond)},
	}); err != nil {
		t.Fatalf("AddMessages err = %v", err)
	}

	service.touchActiveSession(userID, session.SessionID)
	service.processRegenerateCommand(context.Background(), cfg, userID)

	record, ok := service.handler.chatManager.GetSession(session.SessionID)
	if !ok {
		t.Fatal("session not found after regenerate send failure")
	}
	if len(record.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(record.Messages))
	}
	if record.Messages[1].Role != "assistant" || record.Messages[1].Content != "old reply" {
		t.Fatalf("assistant message = %#v, want old reply preserved", record.Messages[1])
	}
	if chatRequests != 1 {
		t.Fatalf("chat request count = %d, want 1", chatRequests)
	}
	mu.Lock()
	defer mu.Unlock()
	if sendAttempts != 1 {
		t.Fatalf("send attempts = %d, want 1", sendAttempts)
	}
}

func TestHandleRegenerateCommand_RejectsWhenPendingReplyBusy(t *testing.T) {
	server, state := newClawBotTestServer(t, "fresh reply")
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Session", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	if err := service.handler.chatManager.AddMessages(session.SessionID, []storage.ChatMessage{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
		{Role: "assistant", Content: "old reply", Timestamp: time.Now().Add(time.Millisecond)},
	}); err != nil {
		t.Fatalf("AddMessages err = %v", err)
	}

	service.touchActiveSession(userID, session.SessionID)
	service.pendingReplies[userID] = &clawBotPendingReply{
		Messages:   []clawBotPendingMessage{{Text: "queued"}},
		LastActive: time.Now(),
	}

	service.handleRegenerateCommand(context.Background(), cfg, userID, "")

	state.mu.Lock()
	defer state.mu.Unlock()

	if len(state.chatRequests) != 0 {
		t.Fatalf("chat request count = %d, want 0", len(state.chatRequests))
	}
	if len(state.sendTexts) != 1 {
		t.Fatalf("send text count = %d, want 1", len(state.sendTexts))
	}
	if !strings.Contains(state.sendTexts[0], "待处理消息") {
		t.Fatalf("sent text = %q, want busy hint", state.sendTexts[0])
	}

	record, ok := service.handler.chatManager.GetSession(session.SessionID)
	if !ok {
		t.Fatal("session not found after busy regenerate")
	}
	if len(record.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(record.Messages))
	}
	if record.Messages[1].Content != "old reply" {
		t.Fatalf("assistant content = %q, want %q", record.Messages[1].Content, "old reply")
	}
}

func TestHandleIncomingMessage_RegenerateBusyRejectsSameUserNewMessage(t *testing.T) {
	chatStarted := make(chan struct{}, 1)
	releaseChat := make(chan struct{})
	replySent := make(chan struct{}, 1)
	busySent := make(chan string, 1)

	var state struct {
		mu           sync.Mutex
		chatRequests int
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			state.mu.Lock()
			state.chatRequests++
			state.mu.Unlock()

			select {
			case chatStarted <- struct{}{}:
			default:
			}

			<-releaseChat

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
							Content: "fresh reply",
						},
						FinishReason: "stop",
					},
				},
			})
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			text := ""
			if len(req.Msg.ItemList) > 0 && req.Msg.ItemList[0].TextItem != nil {
				text = req.Msg.ItemList[0].TextItem.Text
			}
			if strings.Contains(text, "重新生成上一轮回复") {
				select {
				case busySent <- text:
				default:
				}
			}
			if text == "fresh reply" {
				select {
				case replySent <- struct{}{}:
				default:
				}
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	userID := "wx-user"
	sessionID := generateClawBotSessionID(userID)
	session, err := service.handler.chatManager.CreateSession(sessionID, "Session", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	if err := service.handler.chatManager.AddMessages(session.SessionID, []storage.ChatMessage{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
		{Role: "assistant", Content: "old reply", Timestamp: time.Now().Add(time.Millisecond)},
	}); err != nil {
		t.Fatalf("AddMessages err = %v", err)
	}
	service.touchActiveSession(userID, session.SessionID)

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage(userID, "/re"))

	select {
	case <-chatStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for regenerate chat request")
	}

	service.handleIncomingMessage(context.Background(), cfg, newClawBotTextIncomingMessage(userID, "later message"))

	select {
	case text := <-busySent:
		if !strings.Contains(text, "重新生成上一轮回复") {
			t.Fatalf("busy text = %q, want regenerate busy hint", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for busy hint")
	}

	close(releaseChat)

	select {
	case <-replySent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for regenerate reply")
	}

	waitForAssistantMessageContent(t, service.handler.chatManager, session.SessionID, "fresh reply")

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.chatRequests != 1 {
		t.Fatalf("chat request count = %d, want 1", state.chatRequests)
	}
}

func TestPollLoop_RegenerateDoesNotBlockOtherUsers(t *testing.T) {
	regenStarted := make(chan struct{}, 1)
	releaseRegen := make(chan struct{})
	user1Sent := make(chan struct{}, 1)
	user2Sent := make(chan struct{}, 1)

	var (
		mu           sync.Mutex
		updatesCalls int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			mu.Lock()
			updatesCalls++
			call := updatesCalls
			mu.Unlock()

			resp := client.ClawBotGetUpdatesResponse{ErrCode: 0, GetUpdatesBuf: "cursor-1"}
			if call == 1 {
				resp.Msgs = []client.ClawBotIncomingMessage{
					newClawBotTextIncomingMessage("user-1", "/re"),
					newClawBotTextIncomingMessage("user-2", "hello user2"),
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/chat/completions":
			var req struct {
				Messages []struct {
					Role    string      `json:"role"`
					Content interface{} `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request failed: %v", err)
			}

			lastUserText := ""
			for i := len(req.Messages) - 1; i >= 0; i-- {
				if req.Messages[i].Role == "user" {
					if content, ok := req.Messages[i].Content.(string); ok {
						lastUserText = content
					}
					break
				}
			}

			switch lastUserText {
			case "needs regen":
				select {
				case regenStarted <- struct{}{}:
				default:
				}
				<-releaseRegen
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(client.ChatResponse{
					ID:      "chatcmpl-regen",
					Object:  "chat.completion",
					Created: time.Now().Unix(),
					Model:   "gpt-test",
					Choices: []client.Choice{{Message: client.Message{Role: "assistant", Content: "fresh reply user1"}}},
				})
			case "hello user2":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(client.ChatResponse{
					ID:      "chatcmpl-user2",
					Object:  "chat.completion",
					Created: time.Now().Unix(),
					Model:   "gpt-test",
					Choices: []client.Choice{{Message: client.Message{Role: "assistant", Content: "reply user2"}}},
				})
			default:
				t.Fatalf("unexpected last user text in chat request: %q", lastUserText)
			}
		case "/ilink/bot/sendmessage":
			var req client.ClawBotSendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode sendmessage request failed: %v", err)
			}

			switch req.Msg.ToUserID {
			case "user-1":
				select {
				case user1Sent <- struct{}{}:
				default:
				}
			case "user-2":
				select {
				case user2Sent <- struct{}{}:
				default:
				}
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.ClawBotSendMessageResponse{Ret: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTestClawBotService(t, server.URL)
	cfg := service.handler.configManager.GetClawBotConfig()

	sessionID := generateClawBotSessionID("user-1")
	session, err := service.handler.chatManager.CreateSession(sessionID, "Session", "", "")
	if err != nil {
		t.Fatalf("CreateSession err = %v", err)
	}
	if err := service.handler.chatManager.AddMessages(session.SessionID, []storage.ChatMessage{
		{Role: "user", Content: "needs regen", Timestamp: time.Now()},
		{Role: "assistant", Content: "old reply", Timestamp: time.Now().Add(time.Millisecond)},
	}); err != nil {
		t.Fatalf("AddMessages err = %v", err)
	}
	service.touchActiveSession("user-1", session.SessionID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.pollLoop(ctx, cfg)

	select {
	case <-regenStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for regenerate request to start")
	}

	select {
	case <-user2Sent:
	case <-time.After(2 * time.Second):
		t.Fatal("user-2 reply was blocked by user-1 regenerate")
	}

	close(releaseRegen)

	select {
	case <-user1Sent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for user-1 regenerate reply")
	}

	cancel()
}
