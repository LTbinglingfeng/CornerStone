package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
		configManager: configManager,
		promptManager: newTestPromptManager(t),
		chatManager:   storage.NewChatManager(t.TempDir()),
		userManager:   newTestUserManager(t),
	}

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
