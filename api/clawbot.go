package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
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

type clawBotConversation struct {
	Messages   []client.Message
	LastActive time.Time
}

type ClawBotService struct {
	handler *Handler
	client  *client.ClawBotClient

	mu            sync.RWMutex
	qrSessions    map[string]clawBotQRCodeSession
	conversations map[string]*clawBotConversation
	contextTokens map[string]string
	wechatUIN     string

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
		handler:       handler,
		client:        client.NewClawBotClient(),
		qrSessions:    make(map[string]clawBotQRCodeSession),
		conversations: make(map[string]*clawBotConversation),
		contextTokens: make(map[string]string),
		wechatUIN:     wechatUIN,
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
	history := s.appendUserMessage(userID, text)

	reply, err := s.generateReply(ctx, cfg.PromptID, history)
	if err != nil {
		logging.Errorf("clawbot generate reply failed: user=%s err=%v", userID, err)
		s.setLastError(err)
		reply = "暂时无法处理你的消息，请稍后再试。"
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	chunks := splitClawBotReply(reply, clawBotReplyChunkMaxRune)
	if len(chunks) == 0 {
		return
	}

	contextToken = s.getContextToken(userID)
	for _, chunk := range chunks {
		if err := s.client.SendTextMessage(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, userID, contextToken, chunk); err != nil {
			s.setLastError(err)
			logging.Errorf("clawbot send reply failed: user=%s err=%v", userID, err)
			return
		}
	}

	s.appendAssistantMessage(userID, reply)
}

func (s *ClawBotService) generateReply(ctx context.Context, promptID string, history []client.Message) (string, error) {
	provider := s.handler.configManager.GetActiveProvider()
	if provider == nil {
		return "", fmt.Errorf("no active provider configured")
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		return "", fmt.Errorf("active provider is not chat-capable")
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return "", fmt.Errorf("api key not configured")
	}

	systemPrompt := strings.TrimSpace(s.handler.configManager.GetSystemPrompt())
	persona := ""
	if promptID != "" {
		if prompt, ok := s.handler.promptManager.Get(promptID); ok {
			persona = strings.TrimSpace(prompt.Content)
		}
	}
	cfg := s.handler.configManager.Get()
	if cfg.MemoryEnabled && promptID != "" && s.handler.memoryManager != nil {
		persona = storage.BuildPromptWithMemory(persona, s.handler.memoryManager.GetActiveMemories(promptID))
	}

	channelGuide := strings.TrimSpace(`[渠道说明]
你正在通过微信 ClawBot 渠道回复用户。
请直接输出可以发送给用户的自然语言消息。
不要使用红包、拍一拍等仅界面渠道支持的能力。`)

	var fullSystemPrompt string
	if systemPrompt != "" {
		fullSystemPrompt = systemPrompt
	}
	if persona != "" {
		if fullSystemPrompt != "" {
			fullSystemPrompt += "\n\n"
		}
		fullSystemPrompt += "[人设]\n" + persona
	}
	if channelGuide != "" {
		if fullSystemPrompt != "" {
			fullSystemPrompt += "\n\n"
		}
		fullSystemPrompt += channelGuide
	}

	messages := make([]client.Message, 0, len(history)+1)
	if strings.TrimSpace(fullSystemPrompt) != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: strings.TrimSpace(fullSystemPrompt),
		})
	}
	messages = append(messages, limitMessagesByTurns(history, provider.ContextMessages)...)
	resolvedMessages, err := s.handler.prepareMessagesForProvider(messages, provider.ImageCapable)
	if err != nil {
		return "", err
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
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (s *ClawBotService) appendUserMessage(userID, text string) []client.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv := s.getConversationLocked(userID)
	conv.Messages = append(conv.Messages, client.Message{
		Role:    "user",
		Content: text,
	})
	conv.LastActive = time.Now()
	conv.Messages = limitMessagesByTurns(conv.Messages, 64)
	return append([]client.Message(nil), conv.Messages...)
}

func (s *ClawBotService) appendAssistantMessage(userID, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv := s.getConversationLocked(userID)
	conv.Messages = append(conv.Messages, client.Message{
		Role:    "assistant",
		Content: text,
	})
	conv.LastActive = time.Now()
	conv.Messages = limitMessagesByTurns(conv.Messages, 64)
}

func (s *ClawBotService) getConversationLocked(userID string) *clawBotConversation {
	if conv, ok := s.conversations[userID]; ok {
		return conv
	}
	conv := &clawBotConversation{
		Messages:   make([]client.Message, 0, 8),
		LastActive: time.Now(),
	}
	s.conversations[userID] = conv
	return conv
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
	for userID, conv := range s.conversations {
		if now.Sub(conv.LastActive) > clawBotConversationIdle {
			delete(s.conversations, userID)
			delete(s.contextTokens, userID)
		}
	}
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
