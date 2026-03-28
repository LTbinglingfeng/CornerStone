package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	clawBotQRCodeSessionTTL  = 5 * time.Minute
	clawBotConversationIdle  = 30 * time.Minute
	clawBotCleanupInterval   = 5 * time.Minute
	clawBotReplyChunkMaxRune = 2000
	clawBotProcessTimeout    = 2 * time.Minute
)

type ClawBotSettingsResponse struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"base_url"`
	BotToken    string `json:"bot_token,omitempty"`
	HasBotToken bool   `json:"has_bot_token"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
	PromptID    string `json:"prompt_id,omitempty"`
	PromptName  string `json:"prompt_name,omitempty"`
	Status      string `json:"status"`
	Polling     bool   `json:"polling"`
	LastError   string `json:"last_error,omitempty"`
	LastErrorAt string `json:"last_error_at,omitempty"`
}

type ClawBotQRCodeStartResponse struct {
	SessionID        string `json:"session_id"`
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content,omitempty"`
}

type ClawBotQRCodePollResponse struct {
	Status   string                   `json:"status"`
	Settings *ClawBotSettingsResponse `json:"settings,omitempty"`
}

type clawBotQRCodeSession struct {
	ID               string
	BaseURL          string
	QRCode           string
	QRCodeImgContent string
	CreatedAt        time.Time
}

type clawBotActiveSession struct {
	SessionID  string
	LastActive time.Time
}

type clawBotPendingReply struct {
	Messages        []string
	WindowStartedAt time.Time
	LastActive      time.Time
	Timer           *time.Timer
	Processing      bool
	Ready           bool
}

type ClawBotService struct {
	handler *Handler
	client  *client.ClawBotClient

	mu             sync.RWMutex
	qrSessions     map[string]clawBotQRCodeSession
	activeSessions map[string]*clawBotActiveSession
	pendingReplies map[string]*clawBotPendingReply
	contextTokens  map[string]string
	wechatUIN      string

	workerCancel  context.CancelFunc
	workerRunning bool
	lastError     string
	lastErrorAt   time.Time
}

func NewClawBotService(handler *Handler) *ClawBotService {
	wechatUIN, err := client.GenerateClawBotWechatUIN()
	if err != nil {
		logging.Errorf("clawbot generate wechat uin failed: %v", err)
	}

	s := &ClawBotService{
		handler:        handler,
		client:         client.NewClawBotClient(),
		qrSessions:     make(map[string]clawBotQRCodeSession),
		activeSessions: make(map[string]*clawBotActiveSession),
		pendingReplies: make(map[string]*clawBotPendingReply),
		contextTokens:  make(map[string]string),
		wechatUIN:      wechatUIN,
	}
	s.ApplyCurrentConfig()
	go s.cleanupLoop()
	return s
}

func (s *ClawBotService) Close() {
	s.mu.Lock()
	cancel := s.workerCancel
	s.workerCancel = nil
	s.workerRunning = false
	s.mu.Unlock()

	s.clearAllPendingReplies()

	if cancel != nil {
		cancel()
	}
}

func (s *ClawBotService) ApplyCurrentConfig() {
	cfg := s.handler.configManager.GetClawBotConfig()
	s.applyConfig(cfg)
}

func (s *ClawBotService) applyConfig(cfg config.ClawBotConfig) {
	s.mu.Lock()
	cancel := s.workerCancel
	s.workerCancel = nil
	s.workerRunning = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}

	ctx, workerCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.workerCancel = workerCancel
	s.workerRunning = true
	s.mu.Unlock()

	go s.pollLoop(ctx, cfg)
}

func (s *ClawBotService) StartQRCode(ctx context.Context, baseURL string) (*ClawBotQRCodeStartResponse, error) {
	s.cleanupState()

	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = s.handler.configManager.GetClawBotConfig().BaseURL
	}
	if baseURL == "" {
		baseURL = config.DefaultClawBotBaseURL
	}

	resp, err := s.client.GetBotQRCode(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	qrcode := strings.TrimSpace(resp.QRCode)
	qrcodeImgContent := strings.TrimSpace(resp.QRCodeImgContent)
	if qrcode == "" {
		return nil, fmt.Errorf("clawbot qrcode is empty")
	}

	sessionID := generateID()
	session := clawBotQRCodeSession{
		ID:               sessionID,
		BaseURL:          baseURL,
		QRCode:           qrcode,
		QRCodeImgContent: qrcodeImgContent,
		CreatedAt:        time.Now(),
	}

	s.mu.Lock()
	s.qrSessions[sessionID] = session
	s.mu.Unlock()

	return &ClawBotQRCodeStartResponse{
		SessionID:        sessionID,
		QRCode:           qrcode,
		QRCodeImgContent: qrcodeImgContent,
	}, nil
}

func (s *ClawBotService) PollQRCode(ctx context.Context, sessionID string) (*ClawBotQRCodePollResponse, error) {
	s.cleanupState()

	s.mu.RLock()
	session, ok := s.qrSessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("qrcode session not found")
	}

	resp, err := s.client.GetQRCodeStatus(ctx, session.BaseURL, session.QRCode)
	if err != nil {
		return nil, err
	}

	status := strings.TrimSpace(resp.Status)
	result := &ClawBotQRCodePollResponse{
		Status: status,
	}
	if status == "expired" {
		s.mu.Lock()
		delete(s.qrSessions, sessionID)
		s.mu.Unlock()
		return result, nil
	}
	if status != "confirmed" {
		return result, nil
	}

	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.BaseURL = strings.TrimSpace(resp.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = session.BaseURL
	}
	cfg.BotToken = strings.TrimSpace(resp.BotToken)
	cfg.ILinkUserID = strings.TrimSpace(resp.ILinkUserID)
	cfg.GetUpdatesBuf = ""
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		return nil, err
	}
	s.ApplyCurrentConfig()

	settings, err := s.GetSettings()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.qrSessions, sessionID)
	s.mu.Unlock()
	result.Settings = settings
	return result, nil
}

func (s *ClawBotService) GetSettings() (*ClawBotSettingsResponse, error) {
	cfg := s.handler.configManager.GetClawBotConfig()

	promptName := ""
	if cfg.PromptID != "" {
		if prompt, ok := s.handler.promptManager.Get(cfg.PromptID); ok {
			promptName = prompt.Name
		}
	}

	s.mu.RLock()
	polling := s.workerRunning
	lastError := s.lastError
	lastErrorAt := s.lastErrorAt
	s.mu.RUnlock()

	status := "disabled"
	switch {
	case !cfg.Enabled:
		status = "disabled"
	case strings.TrimSpace(cfg.BotToken) == "":
		status = "missing_token"
	case polling:
		status = "running"
	case lastError != "":
		status = "error"
	default:
		status = "stopped"
	}

	resp := &ClawBotSettingsResponse{
		Enabled:     cfg.Enabled,
		BaseURL:     cfg.BaseURL,
		BotToken:    maskClawBotSecret(cfg.BotToken),
		HasBotToken: strings.TrimSpace(cfg.BotToken) != "",
		ILinkUserID: cfg.ILinkUserID,
		PromptID:    cfg.PromptID,
		PromptName:  promptName,
		Status:      status,
		Polling:     polling,
		LastError:   lastError,
	}
	if !lastErrorAt.IsZero() {
		resp.LastErrorAt = lastErrorAt.Format(time.RFC3339)
	}
	return resp, nil
}

func (s *ClawBotService) pollLoop(ctx context.Context, cfg config.ClawBotConfig) {
	cursor := strings.TrimSpace(cfg.GetUpdatesBuf)
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		resp, err := s.client.GetUpdates(ctx, cfg.BaseURL, cfg.BotToken, cursor, s.wechatUIN)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.setLastError(err)
			time.Sleep(2 * time.Second)
			continue
		}

		s.clearLastError()
		if resp.ErrCode == -14 {
			cursor = ""
			continue
		}
		if resp.ErrCode != 0 {
			s.setLastError(fmt.Errorf("clawbot getupdates errcode=%d", resp.ErrCode))
			time.Sleep(2 * time.Second)
			continue
		}

		nextCursor := strings.TrimSpace(resp.GetUpdatesBuf)
		if nextCursor != "" {
			cursor = nextCursor
		}

		for _, msg := range resp.Msgs {
			if msg.MessageType != 1 {
				continue
			}
			text := client.ExtractTextFromClawBotMessage(msg)
			if text == "" {
				continue
			}
			s.handleIncomingMessage(ctx, cfg, msg.FromUserID, msg.ContextToken, text)
		}
	}
}

func (s *ClawBotService) handleIncomingMessage(ctx context.Context, cfg config.ClawBotConfig, userID, contextToken, text string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	s.setContextToken(userID, contextToken)
	if isClawBotNewCommand(text) {
		s.clearPendingReply(userID)
		s.handleNewCommand(ctx, cfg, userID)
		return
	}

	batch := s.enqueuePendingMessage(userID, text)
	if len(batch) == 0 {
		return
	}
	s.processPendingBatchAsync(userID, batch)
}

func (s *ClawBotService) handleNewCommand(ctx context.Context, cfg config.ClawBotConfig, userID string) {
	if _, err := s.createAndActivateSession(userID, cfg.PromptID); err != nil {
		logging.Errorf("clawbot create new session failed: user=%s err=%v", userID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法开始新聊天，请稍后再试。")
		return
	}

	if err := s.sendTextReply(ctx, cfg, userID, "已开始新聊天"); err != nil {
		s.setLastError(err)
		logging.Errorf("clawbot send new session confirmation failed: user=%s err=%v", userID, err)
	}
}

func (s *ClawBotService) processIncomingBatch(ctx context.Context, cfg config.ClawBotConfig, userID string, texts []string) {
	if len(texts) == 0 {
		return
	}

	session, err := s.getOrCreateActiveSession(userID, cfg.PromptID)
	if err != nil {
		logging.Errorf("clawbot prepare session failed: user=%s err=%v", userID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法开始聊天，请稍后再试。")
		return
	}

	now := time.Now()
	storageMessages := make([]storage.ChatMessage, 0, len(texts))
	for index, text := range texts {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		storageMessages = append(storageMessages, storage.ChatMessage{
			Role:      "user",
			Content:   trimmed,
			Timestamp: now.Add(time.Millisecond * time.Duration(index)),
		})
	}
	if len(storageMessages) == 0 {
		return
	}

	if err := s.handler.chatManager.AddMessages(session.SessionID, storageMessages); err != nil {
		logging.Errorf("clawbot save user messages failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法处理你的消息，请稍后再试。")
		return
	}

	reply, memSession, err := s.generateReply(ctx, session.SessionID, cfg.PromptID)
	if err != nil {
		logging.Errorf("clawbot generate reply failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		reply = "暂时无法处理你的消息，请稍后再试。"
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	if err := s.sendTextReply(ctx, cfg, userID, reply); err != nil {
		s.setLastError(err)
		logging.Errorf("clawbot send reply failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		return
	}

	if err := s.handler.chatManager.AddMessage(session.SessionID, "assistant", reply); err != nil {
		logging.Errorf("clawbot save assistant message failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		return
	}

	if memSession != nil {
		memSession.OnRoundComplete()
	}
}

func (s *ClawBotService) generateReply(ctx context.Context, sessionID, configuredPromptID string) (string, *storage.MemorySession, error) {
	provider := s.handler.configManager.GetActiveProvider()
	if provider == nil {
		return "", nil, fmt.Errorf("no active provider configured")
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		return "", nil, fmt.Errorf("active provider is not chat-capable")
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return "", nil, fmt.Errorf("api key not configured")
	}

	session, ok := s.handler.chatManager.GetSession(sessionID)
	if !ok {
		return "", nil, fmt.Errorf("session not found: %s", sessionID)
	}

	systemPrompt := s.handler.configManager.GetSystemPrompt()
	userContext := buildChatUserContext(s.handler.userManager.Get())
	effectivePromptID := strings.TrimSpace(session.PromptID)
	if effectivePromptID == "" {
		effectivePromptID = strings.TrimSpace(configuredPromptID)
	}
	persona := ""
	validPromptID := ""
	if effectivePromptID != "" {
		if prompt, ok := s.handler.promptManager.Get(effectivePromptID); ok {
			persona = strings.TrimSpace(prompt.Content)
			validPromptID = effectivePromptID
		}
	}

	memSession := s.handler.getOrCreateMemorySession(validPromptID, sessionID)
	if memSession != nil {
		persona = storage.BuildPromptWithMemory(persona, memSession.GetActiveMemories())
	}

	channelGuide := strings.TrimSpace(`[渠道说明]
你正在通过微信 ClawBot 渠道回复用户。
你只能回复适合微信文本消息的内容。
请直接输出可以发送给用户的自然语言消息。
不要调用网页端工具能力，包括红包、拍一拍、朋友圈等。`)

	history := convertChatMessages(session.Messages)
	history = mergeTrailingUserMessages(history)
	history = limitMessagesByTurns(history, provider.ContextMessages)

	fullSystemPrompt := buildChatSystemPrompt(systemPrompt, userContext, persona, channelGuide)

	messages := make([]client.Message, 0, len(history)+1)
	if strings.TrimSpace(fullSystemPrompt) != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: strings.TrimSpace(fullSystemPrompt),
		})
	}
	messages = append(messages, history...)
	messages = normalizeMessagesForProvider(messages)
	resolvedMessages, err := s.handler.prepareMessagesForProvider(messages, provider.ImageCapable)
	if err != nil {
		return "", memSession, err
	}

	aiClient := newAIClientForProvider(provider)
	temperature := provider.Temperature
	if provider.Type == config.ProviderTypeAnthropic {
		temperature = 1
	}
	req := client.ChatRequest{
		Model:       provider.Model,
		Messages:    resolvedMessages,
		Stream:      false,
		Temperature: temperature,
		TopP:        provider.TopP,
		Tools:       getChatTools(chatToolOptions{Channel: chatToolChannelClawBot}),
	}
	switch provider.Type {
	case config.ProviderTypeAnthropic:
		req.ThinkingBudget = provider.ThinkingBudget
	case config.ProviderTypeGemini:
		geminiMode := "none"
		geminiLevel := "low"
		geminiBudget := 128
		if provider.GeminiThinkingMode != nil {
			geminiMode = *provider.GeminiThinkingMode
		}
		if provider.GeminiThinkingLevel != nil {
			geminiLevel = *provider.GeminiThinkingLevel
		}
		if provider.GeminiThinkingBudget != nil {
			geminiBudget = *provider.GeminiThinkingBudget
		}
		req.GeminiThinkingMode = geminiMode
		req.GeminiThinkingLevel = geminiLevel
		req.GeminiThinkingBudget = geminiBudget
	default:
		req.ReasoningEffort = provider.ReasoningEffort
	}

	resp, err := aiClient.Chat(ctx, req)
	if err != nil {
		return "", memSession, err
	}
	if len(resp.Choices) == 0 {
		return "", memSession, nil
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), memSession, nil
}

func (s *ClawBotService) processPendingBatchAsync(userID string, batch []string) {
	if len(batch) == 0 {
		s.finishPendingReplyProcessing(userID)
		return
	}

	go func() {
		defer s.finishPendingReplyProcessing(userID)

		cfg := s.handler.configManager.GetClawBotConfig()
		if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), clawBotProcessTimeout)
		defer cancel()

		s.processIncomingBatch(ctx, cfg, userID, batch)
	}()
}

func (s *ClawBotService) enqueuePendingMessage(userID, text string) []string {
	mode, delay := s.getReplyWaitWindow()
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.pendingReplies[userID]
	if state == nil {
		state = &clawBotPendingReply{}
		s.pendingReplies[userID] = state
	}
	if len(state.Messages) == 0 {
		state.WindowStartedAt = now
	}
	state.Messages = append(state.Messages, text)
	state.LastActive = now

	if delay <= 0 {
		if state.Processing {
			state.Ready = true
			if state.Timer != nil {
				state.Timer.Stop()
				state.Timer = nil
			}
			return nil
		}
		return s.beginPendingProcessingLocked(state)
	}

	s.schedulePendingReplyLocked(userID, state, mode, delay, now)
	return nil
}

func (s *ClawBotService) schedulePendingReplyLocked(userID string, state *clawBotPendingReply, mode string, delay time.Duration, now time.Time) {
	if state == nil || len(state.Messages) == 0 {
		return
	}

	fireAt := now.Add(delay)
	if mode == string(config.ReplyWaitWindowModeFixed) {
		startedAt := state.WindowStartedAt
		if startedAt.IsZero() {
			startedAt = now
			state.WindowStartedAt = startedAt
		}
		fireAt = startedAt.Add(delay)
	}

	waitFor := time.Until(fireAt)
	if waitFor < 0 {
		waitFor = 0
	}

	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	state.Timer = time.AfterFunc(waitFor, func() {
		s.flushPendingReply(userID)
	})
}

func (s *ClawBotService) beginPendingProcessingLocked(state *clawBotPendingReply) []string {
	if state == nil || state.Processing || len(state.Messages) == 0 {
		return nil
	}
	batch := append([]string(nil), state.Messages...)
	state.Messages = nil
	state.WindowStartedAt = time.Time{}
	state.Ready = false
	state.Processing = true
	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	return batch
}

func (s *ClawBotService) flushPendingReply(userID string) {
	s.mu.Lock()
	state := s.pendingReplies[userID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	state.Timer = nil
	if state.Processing {
		state.Ready = true
		s.mu.Unlock()
		return
	}
	batch := s.beginPendingProcessingLocked(state)
	s.mu.Unlock()

	if len(batch) == 0 {
		return
	}
	s.processPendingBatchAsync(userID, batch)
}

func (s *ClawBotService) finishPendingReplyProcessing(userID string) {
	mode, delay := s.getReplyWaitWindow()

	s.mu.Lock()
	state := s.pendingReplies[userID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	state.Processing = false
	shouldFlushNow := state.Ready
	state.Ready = false
	hasMessages := len(state.Messages) > 0
	hasTimer := state.Timer != nil
	if !hasMessages && !hasTimer {
		delete(s.pendingReplies, userID)
	}
	s.mu.Unlock()

	if !hasMessages {
		return
	}
	if shouldFlushNow || delay <= 0 {
		s.flushPendingReply(userID)
		return
	}
	if hasTimer {
		return
	}

	s.mu.Lock()
	state = s.pendingReplies[userID]
	if state != nil && !state.Processing && len(state.Messages) > 0 {
		s.schedulePendingReplyLocked(userID, state, mode, delay, time.Now())
	}
	s.mu.Unlock()
}

func (s *ClawBotService) clearPendingReply(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state, ok := s.pendingReplies[userID]; ok {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

func (s *ClawBotService) clearAllPendingReplies() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, state := range s.pendingReplies {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

func (s *ClawBotService) getReplyWaitWindow() (string, time.Duration) {
	cfg := s.handler.configManager.Get()
	mode := cfg.ReplyWaitWindowMode
	switch mode {
	case string(config.ReplyWaitWindowModeFixed):
	case string(config.ReplyWaitWindowModeSliding):
	default:
		mode = string(config.ReplyWaitWindowModeSliding)
	}
	seconds := cfg.ReplyWaitWindowSeconds
	if seconds < 0 {
		seconds = 0
	}
	return mode, time.Duration(seconds) * time.Second
}

func (s *ClawBotService) sendTextReply(ctx context.Context, cfg config.ClawBotConfig, userID, text string) error {
	chunks := splitClawBotReply(text, clawBotReplyChunkMaxRune)
	if len(chunks) == 0 {
		return nil
	}

	contextToken := s.getContextToken(userID)
	for _, chunk := range chunks {
		if err := s.client.SendTextMessage(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, userID, contextToken, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *ClawBotService) getOrCreateActiveSession(userID, promptID string) (*storage.ChatRecord, error) {
	if sessionID, ok := s.getActiveSessionID(userID); ok {
		if session, exists := s.handler.chatManager.GetSession(sessionID); exists {
			s.touchActiveSession(userID, sessionID)
			return session, nil
		}
		return s.createAndActivateSession(userID, promptID)
	}

	if session, ok := s.findLatestSessionForUser(userID); ok {
		s.touchActiveSession(userID, session.SessionID)
		return session, nil
	}

	return s.createAndActivateSession(userID, promptID)
}

func (s *ClawBotService) createAndActivateSession(userID, promptID string) (*storage.ChatRecord, error) {
	session, err := s.createSessionForUser(userID, promptID)
	if err != nil {
		return nil, err
	}
	s.touchActiveSession(userID, session.SessionID)
	return session, nil
}

func (s *ClawBotService) createSessionForUser(userID, promptID string) (*storage.ChatRecord, error) {
	promptID = strings.TrimSpace(promptID)
	promptName := ""
	if promptID != "" {
		if prompt, ok := s.handler.promptManager.Get(promptID); ok {
			promptName = prompt.Name
		} else {
			promptID = ""
		}
	}

	for attempt := 0; attempt < 8; attempt++ {
		sessionID := generateClawBotSessionID(userID)
		if _, exists := s.handler.chatManager.GetSession(sessionID); exists {
			continue
		}
		return s.handler.chatManager.CreateSession(sessionID, "New Chat", promptID, promptName)
	}

	return nil, fmt.Errorf("failed to allocate unique clawbot session id for user %s", userID)
}

func (s *ClawBotService) findLatestSessionForUser(userID string) (*storage.ChatRecord, bool) {
	prefix := clawBotSessionPrefix(userID)
	sessions := s.handler.chatManager.ListSessions()

	latestID := ""
	var latestUpdated time.Time
	for _, session := range sessions {
		if !strings.HasPrefix(session.ID, prefix) {
			continue
		}
		if latestID == "" || session.UpdatedAt.After(latestUpdated) {
			latestID = session.ID
			latestUpdated = session.UpdatedAt
		}
	}
	if latestID == "" {
		return nil, false
	}

	record, ok := s.handler.chatManager.GetSession(latestID)
	return record, ok
}

func (s *ClawBotService) getActiveSessionID(userID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.activeSessions[userID]
	if !ok || state == nil || strings.TrimSpace(state.SessionID) == "" {
		return "", false
	}
	return state.SessionID, true
}

func (s *ClawBotService) touchActiveSession(userID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeSessions == nil {
		s.activeSessions = make(map[string]*clawBotActiveSession)
	}
	s.activeSessions[userID] = &clawBotActiveSession{
		SessionID:  sessionID,
		LastActive: time.Now(),
	}
}

func (s *ClawBotService) setContextToken(userID, contextToken string) {
	contextToken = strings.TrimSpace(contextToken)
	if contextToken == "" {
		return
	}
	s.mu.Lock()
	s.contextTokens[userID] = contextToken
	s.mu.Unlock()
}

func (s *ClawBotService) getContextToken(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contextTokens[userID]
}

func (s *ClawBotService) setLastError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastError = err.Error()
	s.lastErrorAt = time.Now()
	s.mu.Unlock()
}

func (s *ClawBotService) clearLastError() {
	s.mu.Lock()
	s.lastError = ""
	s.lastErrorAt = time.Time{}
	s.mu.Unlock()
}

func (s *ClawBotService) cleanupLoop() {
	ticker := time.NewTicker(clawBotCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupState()
	}
}

func (s *ClawBotService) cleanupState() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.qrSessions {
		if now.Sub(session.CreatedAt) > clawBotQRCodeSessionTTL {
			delete(s.qrSessions, id)
		}
	}
	for userID, session := range s.activeSessions {
		if now.Sub(session.LastActive) > clawBotConversationIdle {
			delete(s.activeSessions, userID)
			delete(s.contextTokens, userID)
		}
	}
	for userID, state := range s.pendingReplies {
		if state == nil {
			delete(s.pendingReplies, userID)
			continue
		}
		if state.Processing {
			continue
		}
		if now.Sub(state.LastActive) <= clawBotConversationIdle {
			continue
		}
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

const clawBotCommandTrimChars = " \t\r\n\v\f\u00a0\ufeff\u200b\u200c\u200d"
const clawBotCommandTrailingPunctuation = "。．.，,！!？?；;：:、~～"

func isClawBotNewCommand(text string) bool {
	normalized := strings.Trim(text, clawBotCommandTrimChars)
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, "／") {
		normalized = "/" + strings.TrimPrefix(normalized, "／")
	}
	normalizedLower := strings.ToLower(normalized)
	if !strings.HasPrefix(normalizedLower, "/new") {
		return false
	}

	rest := normalized[len("/new"):]
	rest = strings.Trim(rest, clawBotCommandTrimChars)
	rest = strings.Trim(rest, clawBotCommandTrailingPunctuation)
	rest = strings.Trim(rest, clawBotCommandTrimChars)
	return rest == ""
}

func clawBotSessionPrefix(userID string) string {
	hash := sha1.Sum([]byte(strings.TrimSpace(userID)))
	return "clawbot_" + hex.EncodeToString(hash[:8]) + "_"
}

func generateClawBotSessionID(userID string) string {
	return clawBotSessionPrefix(userID) + generateID()
}

func newAIClientForProvider(provider *config.Provider) client.AIClient {
	switch provider.Type {
	case config.ProviderTypeOpenAIResponse:
		return client.NewResponsesClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeGemini:
		return client.NewGeminiClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeAnthropic:
		return client.NewAnthropicClient(provider.BaseURL, provider.APIKey)
	default:
		return client.NewClient(provider.BaseURL, provider.APIKey)
	}
}

func splitClawBotReply(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return nil
	}

	remaining := []rune(text)
	chunks := make([]string, 0, 4)
	for len(remaining) > 0 {
		if len(remaining) <= maxRunes {
			chunk := strings.TrimSpace(string(remaining))
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			break
		}

		cut := maxRunes
		for i := maxRunes; i > 0; i-- {
			if remaining[i-1] == '\n' {
				cut = i
				break
			}
		}

		chunk := strings.TrimSpace(string(remaining[:cut]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = remaining[cut:]
		for len(remaining) > 0 && (remaining[0] == '\n' || remaining[0] == '\r' || remaining[0] == ' ') {
			remaining = remaining[1:]
		}
	}
	return chunks
}

func maskClawBotSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
