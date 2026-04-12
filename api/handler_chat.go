package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/internal/search"
	"cornerstone/internal/search/providers"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ChatMessageRequest 前端发送的聊天请求
type ChatMessageRequest struct {
	SessionID   string           `json:"session_id,omitempty"`
	PromptID    string           `json:"prompt_id,omitempty"` // 选择的人设ID
	Messages    []client.Message `json:"messages"`
	Stream      *bool            `json:"stream,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	SaveHistory bool             `json:"save_history,omitempty"`
	Regenerate  bool             `json:"regenerate,omitempty"` // 重新生成最后一轮 AI 响应
}

func buildChatUserContext(userInfo *storage.UserInfo) string {
	if userInfo == nil {
		return ""
	}

	var sections []string
	if userInfo.Username != "" {
		sections = append(sections, fmt.Sprintf("用户名: %s", userInfo.Username))
	}
	if userInfo.Description != "" {
		sections = append(sections, fmt.Sprintf("用户自我描述: %s", userInfo.Description))
	}
	if len(sections) == 0 {
		return ""
	}

	return "[用户信息]\n" + strings.Join(sections, "\n")
}

func buildChatSystemPrompt(systemPrompt, userContext, persona string, extraSections ...string) string {
	sections := make([]string, 0, 3+len(extraSections))

	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		sections = append(sections, trimmed)
	}
	if trimmed := strings.TrimSpace(userContext); trimmed != "" {
		sections = append(sections, trimmed)
	}
	if trimmed := strings.TrimSpace(persona); trimmed != "" {
		sections = append(sections, "[人设]\n"+trimmed)
	}
	for _, extra := range extraSections {
		if trimmed := strings.TrimSpace(extra); trimmed != "" {
			sections = append(sections, trimmed)
		}
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req ChatMessageRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	provider := h.configManager.GetActiveProvider()
	if provider == nil {
		logging.Errorf("chat no active provider")
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "No active provider configured"})
		return
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		logging.Errorf("chat invalid provider type: type=%s", provider.Type)
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Active provider is not chat-capable"})
		return
	}
	if provider.APIKey == "" {
		logging.Errorf("chat no API key: provider=%s", provider.ID)
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "API key not configured"})
		return
	}

	systemPrompt := h.configManager.GetSystemPrompt()

	// 获取用户信息，构建用户上下文
	userContext := buildChatUserContext(h.userManager.Get())

	// 生成或使用会话ID
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateID()
	}

	existingSession, hasExistingSession := h.chatManager.GetSession(sessionID)
	existingMessageCount := 0
	if hasExistingSession {
		existingMessageCount = len(existingSession.Messages)
	}

	// 获取人设（prompt）
	effectivePromptID := req.PromptID
	if effectivePromptID == "" && hasExistingSession {
		effectivePromptID = existingSession.PromptID
	}
	var persona string
	promptName := ""
	if effectivePromptID != "" {
		if prompt, ok := h.promptManager.Get(effectivePromptID); ok {
			persona = prompt.Content
			promptName = prompt.Name
		} else if hasExistingSession {
			promptName = existingSession.PromptName
		}
	}

	memSession := h.getOrCreateMemorySession(effectivePromptID, sessionID)
	if memSession != nil {
		persona = storage.BuildPromptWithMemory(persona, memSession.GetActiveMemories())
	}

	// 重新生成：删除最后一条 user 之后的整段旧响应
	if req.Regenerate && req.SaveHistory {
		if deleted, errDelete := h.chatManager.DeleteTrailingResponseBatch(sessionID); errDelete != nil {
			logging.Errorf("regenerate delete trailing response batch failed: session=%s err=%v", sessionID, errDelete)
		} else if deleted > 0 {
			logging.Infof("regenerate deleted %d trailing response messages: session=%s", deleted, sessionID)
		}
	}

	// 保存用户消息到历史记录（重新生成时跳过）
	if req.SaveHistory && !req.Regenerate && len(req.Messages) > 0 {
		messagesToSave := req.Messages
		if existingMessageCount > 0 && len(req.Messages) > existingMessageCount {
			messagesToSave = req.Messages[existingMessageCount:]
		}

		now := h.now()
		storageMessages := make([]storage.ChatMessage, 0, len(messagesToSave))
		for index, msg := range messagesToSave {
			if msg.Role != "user" {
				continue
			}
			storageMessages = append(storageMessages, storage.ChatMessage{
				Role:             msg.Role,
				Content:          msg.Content,
				ReasoningContent: msg.ReasoningContent,
				ToolCalls:        msg.ToolCalls,
				ImagePaths:       msg.ImagePaths,
				Timestamp:        now.Add(time.Millisecond * time.Duration(index)),
			})
		}

		if len(storageMessages) > 0 {
			if errAddMessages := h.chatManager.AddMessages(sessionID, storageMessages); errAddMessages != nil {
				if errors.Is(errAddMessages, storage.ErrInvalidID) {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
					return
				}
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errAddMessages.Error()})
				return
			}
			if h.idleGreetingService != nil {
				lastUserAt := storageMessages[len(storageMessages)-1].Timestamp
				if _, errIdleGreeting := h.idleGreetingService.Rebuild(idleGreetingScheduleRequest{
					Channel:    storage.ReminderChannelWeb,
					SessionID:  sessionID,
					PromptID:   effectivePromptID,
					PromptName: promptName,
					Target: storage.ReminderTarget{
						Kind: storage.ReminderTargetKindSession,
					},
					LastUserAt: lastUserAt,
				}); errIdleGreeting != nil {
					logging.Warnf("rebuild idle greeting failed: channel=web session=%s err=%v", sessionID, errIdleGreeting)
				}
			}
		}
	}

	historyMessages := req.Messages
	currentConfig := config.DefaultConfig()
	if h.configManager != nil {
		currentConfig = h.configManager.Get()
	}
	normalizedToolToggles := config.NormalizeToolToggles(currentConfig.ToolToggles)
	availableTools := getChatTools(chatToolOptions{
		ToolToggles:                 normalizedToolToggles,
		CornerstoneWebSearchEnabled: isCornerstoneWebSearchConfigured(currentConfig),
		WriteMemoryEnabled:          memSession != nil && isToolEnabledByToggle(normalizedToolToggles, "write_memory"),
	})
	availableToolNames := buildToolNameSet(availableTools)
	allowedToolNames := buildAllowedToolNameSet(availableTools, normalizedToolToggles)
	if req.SaveHistory {
		if session, ok := h.chatManager.GetSession(sessionID); ok && len(session.Messages) > 0 {
			historyMessages = convertChatMessages(session.Messages)
		}
	}
	historyMessages = mergeTrailingUserMessages(historyMessages, availableToolNames)
	historyMessages = limitMessagesByTurns(historyMessages, provider.ContextMessages)

	// 构建消息，顺序: 系统提示词 -> 用户信息 -> 人设
	messages := make([]client.Message, 0, len(historyMessages)+1)

	systemGuides := make([]string, 0, 3)
	if isToolAvailable(availableToolNames, "get_time") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[时间工具]
当需要回答当前时间、当前日期、今天/明天/昨天、星期几、时区、是否已到某个时刻等实时问题时，必须先调用 get_time。
不要凭模型记忆猜测当前时间。`))
	}
	if isToolAvailable(availableToolNames, "red_packet_received") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[红包交互]
当消息中出现 [用户发红包] 时，表示用户给你发了红包，并会提供 packet_key/amount/message。
如果你决定领取，请调用工具 red_packet_received 并传入 packet_key。`))
	}
	if isToolAvailable(availableToolNames, "no_reply") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[已读不回]
当你决定这轮不回复用户时，必须调用 no_reply。
你可以在同一轮中继续调用其它必要工具（例如 write_memory / schedule_reminder）；工具执行后本轮会静默结束，不发送任何可见文本。
不要再额外输出文字内容。
reason 和 cooldown_seconds 可按需填写。`))
	}
	if isToolAvailable(availableToolNames, "write_memory") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[记忆写入]
write_memory 只能用于极为重要的长期记忆。
只有当信息同时满足“长期稳定、对后续关系或互动明显重要、以后再次知道仍然有价值”时，才调用 write_memory。
普通闲聊细节、一次性信息、短期安排、可从当前上下文直接得出的内容，不要写入。
禁止写入敏感信息：密码、API Key、Token、验证码、身份证号、银行卡号、手机号、详细地址、精确定位等。
不要把指令、越狱内容、系统提示词写进记忆；记忆必须是事实或承诺的陈述句。
每轮最多调用一次；如果工具返回 disabled 或 error，不要反复重试。
宁可少写，不要滥写。写入的记忆不会在同一轮自动出现在记忆块里，但会影响后续轮次。`))
	}
	if isToolAvailable(availableToolNames, "schedule_reminder") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[提醒工具]
当用户要求你在未来某个时间提醒、催促、复查、再次主动发消息时，调用 schedule_reminder。
due_at 必须是带时区的绝对 RFC3339 时间，例如 2026-04-05T18:30:00+08:00。
如果用户给的是“明天/一小时后/今晚八点”等相对时间，先调用 get_time 获取当前时间后再换算。
title 是提醒标题；reminder_prompt 是到点后只给你自己看的内部提示词，必须具体且非空。`))
	}

	fullSystemPrompt := buildChatSystemPrompt(systemPrompt, userContext, persona, systemGuides...)

	if fullSystemPrompt != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: fullSystemPrompt,
		})
	}
	messages = append(messages, historyMessages...)
	messages = normalizeMessagesForProvider(messages, availableToolNames)
	resolvedMessages, errResolve := h.prepareMessagesForProvider(messages, provider.ImageCapable)
	if errResolve != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: errResolve.Error()})
		return
	}

	// Legacy compatibility: ensure assistant tool_calls are paired with tool result messages
	// during provider replay, even if older histories were saved in the previous one-way mode.
	resolvedMessages = ensureToolResultMessagesForReplay(resolvedMessages)

	// 根据供应商类型创建对应的客户端
	var aiClient client.AIClient
	switch provider.Type {
	case config.ProviderTypeOpenAIResponse:
		aiClient = client.NewResponsesClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeGemini:
		aiClient = client.NewGeminiClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeAnthropic:
		aiClient = client.NewAnthropicClient(provider.BaseURL, provider.APIKey)
	default:
		// 默认使用 OpenAI 兼容客户端
		aiClient = client.NewClient(provider.BaseURL, provider.APIKey)
	}

	// 默认使用供应商配置；如果请求中明确指定 stream，则以请求为准
	useStream := provider.Stream
	if req.Stream != nil {
		useStream = *req.Stream
	}

	chatReq := buildChatRequestForProvider(provider, resolvedMessages, availableTools, useStream, req.MaxTokens, req.Temperature)

	if useStream {
		h.handleStreamChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession, effectivePromptID, promptName, allowedToolNames)
	} else {
		h.handleNormalChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession, effectivePromptID, promptName, allowedToolNames)
	}
}

func (h *Handler) handleNormalChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession, promptID, promptName string, allowedToolNames map[string]bool) {
	ctxAI := context.WithoutCancel(r.Context())

	toolExecutor := newChatToolExecutor()
	toolExecutor.memoryManager = h.memoryManager
	toolExecutor.configManager = h.configManager
	toolExecutor.weatherService = h.getWeatherService()
	toolExecutor.exactTimeService = h.exactTimeService
	toolExecutor.reminderService = h.reminderService
	if h.configManager != nil {
		toolExecutor.cornerstoneWebSearch = newCornerstoneWebSearchOrchestrator(h.configManager.Get())
	}
	loopResult, errLoop := runChatWithToolLoop(
		ctxAI,
		aiClient,
		req,
		toolExecutor,
		chatToolContext{
			SessionID:        sessionID,
			PromptID:         promptID,
			PromptName:       promptName,
			Channel:          chatToolChannelDefault,
			MemSession:       memSession,
			AllowedToolNames: allowedToolNames,
		},
		nil,
	)
	if errLoop != nil {
		// Persist any assistant/tool messages already produced before returning error.
		// This closes the "side effects happened but history missing" gap when tool execution succeeded
		// but a later model hop failed.
		if saveHistory && loopResult != nil && len(loopResult.NewMessages) > 0 {
			now := h.now()
			storageMessages := make([]storage.ChatMessage, 0, len(loopResult.NewMessages))
			for index, msg := range loopResult.NewMessages {
				storageMessages = append(storageMessages, storage.ChatMessage{
					Role:             msg.Role,
					Content:          msg.Content,
					ReasoningContent: msg.ReasoningContent,
					ToolCalls:        msg.ToolCalls,
					ToolCallID:       msg.ToolCallID,
					ImagePaths:       msg.ImagePaths,
					TTSAudioPaths:    msg.TTSAudioPaths,
					Timestamp:        now.Add(time.Millisecond * time.Duration(index)),
				})
			}
			ensureAssistantMessageSplitToken(storageMessages, configuredAssistantMessageSplitToken(h.configManager))

			if errSaveHistory := h.chatManager.AddMessages(sessionID, storageMessages); errSaveHistory != nil {
				logging.Errorf("save tool-loop partial history error: %v", errSaveHistory)
				// If the client is already gone, there's nothing to reply with; we still tried to persist.
				if r.Context().Err() != nil {
					return
				}
				if errors.Is(errSaveHistory, storage.ErrInvalidID) {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
					return
				}
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errSaveHistory.Error()})
				return
			}
		}

		if r.Context().Err() != nil {
			logging.Errorf("chat request cancelled (client disconnected): %v", errLoop)
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errLoop.Error()})
		return
	}

	assistantMessageSplitToken := configuredAssistantMessageSplitToken(h.configManager)

	// Generate TTS only for the final assistant message.
	ttsAudioPaths := []string(nil)
	if loopResult != nil && loopResult.FinalResponse != nil && len(loopResult.FinalResponse.Choices) > 0 {
		finalMessage := loopResult.FinalResponse.Choices[0].Message
		ttsAudioPaths = h.maybeGenerateTTSAudio(ctxAI, finalMessage.Content, assistantMessageSplitToken)
		if len(ttsAudioPaths) > 0 {
			finalMessage.TTSAudioPaths = ttsAudioPaths
			loopResult.FinalResponse.Choices[0].Message = finalMessage
			if n := len(loopResult.NewMessages); n > 0 && loopResult.NewMessages[n-1].Role == "assistant" {
				loopResult.NewMessages[n-1].TTSAudioPaths = ttsAudioPaths
			}
		}
	}

	// Prepare messages for persistence and frontend rendering.
	now := h.now()
	storageMessages := make([]storage.ChatMessage, 0, len(loopResult.NewMessages))
	for index, msg := range loopResult.NewMessages {
		storageMessages = append(storageMessages, storage.ChatMessage{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCalls:        msg.ToolCalls,
			ToolCallID:       msg.ToolCallID,
			ImagePaths:       msg.ImagePaths,
			TTSAudioPaths:    msg.TTSAudioPaths,
			Timestamp:        now.Add(time.Millisecond * time.Duration(index)),
		})
	}
	ensureAssistantMessageSplitToken(storageMessages, assistantMessageSplitToken)

	// Save assistant/tool messages as a batch (user messages are saved earlier).
	if saveHistory && len(storageMessages) > 0 {
		if errSaveHistory := h.chatManager.AddMessages(sessionID, storageMessages); errSaveHistory != nil {
			if errors.Is(errSaveHistory, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errSaveHistory.Error()})
			return
		}
	}

	if r.Context().Err() != nil {
		return
	}

	if memSession != nil {
		memSession.OnRoundComplete()
	}

	result := map[string]interface{}{
		"session_id": sessionID,
		"response":   loopResult.FinalResponse,
		"messages":   storageMessages,
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})
}

// handleStreamChat 处理流式聊天 (SSE)
func (h *Handler) handleStreamChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession, promptID, promptName string, allowedToolNames map[string]bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Streaming not supported"})
		return
	}

	ctxAI := context.WithoutCancel(r.Context())
	clientDisconnected := false
	isClientDisconnected := func() bool {
		if clientDisconnected {
			return true
		}
		select {
		case <-r.Context().Done():
			clientDisconnected = true
			return true
		default:
			return false
		}
	}

	// 发送session_id
	sessionPayload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	if !isClientDisconnected() {
		if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", sessionPayload); errWrite != nil {
			clientDisconnected = true
		} else {
			flusher.Flush()
		}
	}

	toolExecutor := newChatToolExecutor()
	toolExecutor.memoryManager = h.memoryManager
	toolExecutor.configManager = h.configManager
	toolExecutor.weatherService = h.getWeatherService()
	toolExecutor.exactTimeService = h.exactTimeService
	toolExecutor.reminderService = h.reminderService
	if h.configManager != nil {
		toolExecutor.cornerstoneWebSearch = newCornerstoneWebSearchOrchestrator(h.configManager.Get())
	}

	assistantMessageSplitToken := configuredAssistantMessageSplitToken(h.configManager)
	baseTime := h.now()
	messageCounter := 0
	createdMessages := make([]storage.ChatMessage, 0, 4)
	finalAssistantTimestamp := ""

	sendEvent := func(payload interface{}) {
		if isClientDisconnected() {
			return
		}
		data, errMarshal := json.Marshal(payload)
		if errMarshal != nil {
			logging.Errorf("marshal stream payload error: %v", errMarshal)
			clientDisconnected = true
			return
		}
		if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", data); errWrite != nil {
			clientDisconnected = true
			return
		}
		flusher.Flush()
	}

	toolExecutor.emitEvent = sendEvent

	appendMessage := func(msg client.Message) {
		ts := baseTime.Add(time.Millisecond * time.Duration(messageCounter))
		messageCounter++

		stored := storage.ChatMessage{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCalls:        msg.ToolCalls,
			ToolCallID:       msg.ToolCallID,
			ImagePaths:       msg.ImagePaths,
			TTSAudioPaths:    msg.TTSAudioPaths,
			Timestamp:        ts,
		}
		if strings.TrimSpace(stored.Role) == "assistant" {
			stored.AssistantMessageSplitToken = assistantMessageSplitTokenPtr(assistantMessageSplitToken)
		}
		createdMessages = append(createdMessages, stored)
		sendEvent(map[string]interface{}{
			"type":    "message",
			"message": stored,
		})
		if msg.Role == "assistant" && len(msg.ToolCalls) == 0 {
			finalAssistantTimestamp = ts.Format(time.RFC3339Nano)
		}
	}

	loopResult, errLoop := runChatWithToolLoop(
		ctxAI,
		aiClient,
		req,
		toolExecutor,
		chatToolContext{
			SessionID:        sessionID,
			PromptID:         promptID,
			PromptName:       promptName,
			Channel:          chatToolChannelDefault,
			MemSession:       memSession,
			AllowedToolNames: allowedToolNames,
		},
		&toolLoopCallbacks{
			OnToolStep: func(step int, assistant client.Message) {
				sendEvent(map[string]interface{}{
					"type": "tool_loop_step",
					"step": step,
				})
				appendMessage(assistant)
			},
			OnToolMessage: func(step int, msg client.Message) {
				appendMessage(msg)
			},
			OnFinalAssistant: func(msg client.Message) {
				appendMessage(msg)
			},
		},
	)

	if errLoop != nil {
		logging.Errorf("Stream error: %v", errLoop)
		sendEvent(map[string]string{"error": errLoop.Error()})
		if saveHistory && len(createdMessages) > 0 {
			if errSaveHistory := h.chatManager.AddMessages(sessionID, createdMessages); errSaveHistory != nil {
				logging.Errorf("save stream partial history error: %v", errSaveHistory)
				errorMessage := errSaveHistory.Error()
				if errors.Is(errSaveHistory, storage.ErrInvalidID) {
					errorMessage = "Invalid session ID"
				}
				sendEvent(map[string]string{"error": errorMessage})
			}
		}
	} else {
		ttsAudioPaths := []string(nil)
		if loopResult != nil && loopResult.FinalResponse != nil && len(loopResult.FinalResponse.Choices) > 0 {
			finalMessage := loopResult.FinalResponse.Choices[0].Message
			ttsAudioPaths = h.maybeGenerateTTSAudio(ctxAI, finalMessage.Content, assistantMessageSplitToken)
			if len(ttsAudioPaths) > 0 {
				finalMessage.TTSAudioPaths = ttsAudioPaths
				loopResult.FinalResponse.Choices[0].Message = finalMessage

				// Update persisted copy (last assistant message).
				for i := len(createdMessages) - 1; i >= 0; i-- {
					if createdMessages[i].Role == "assistant" && len(createdMessages[i].ToolCalls) == 0 {
						createdMessages[i].TTSAudioPaths = ttsAudioPaths
						break
					}
				}

				sendEvent(map[string]interface{}{
					"type":            "tts_audio",
					"timestamp":       finalAssistantTimestamp,
					"tts_audio_paths": ttsAudioPaths,
				})
			}
		}

		if saveHistory && len(createdMessages) > 0 {
			if errSaveHistory := h.chatManager.AddMessages(sessionID, createdMessages); errSaveHistory != nil {
				logging.Errorf("save stream history error: %v", errSaveHistory)
				errorMessage := errSaveHistory.Error()
				if errors.Is(errSaveHistory, storage.ErrInvalidID) {
					errorMessage = "Invalid session ID"
				}
				sendEvent(map[string]string{"error": errorMessage})
			}
		}

		if memSession != nil {
			memSession.OnRoundComplete()
		}
	}

	if !isClientDisconnected() {
		if _, errWrite := fmt.Fprintf(w, "data: [DONE]\n\n"); errWrite != nil {
			clientDisconnected = true
		} else {
			flusher.Flush()
		}
	}
}

func convertChatMessages(messages []storage.ChatMessage) []client.Message {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		if shouldDropEmptyAssistantStorageMessage(msg) {
			continue
		}
		content, imagePaths := buildChatMessageModelInput(msg)
		converted = append(converted, client.Message{
			Role:             msg.Role,
			Content:          content,
			ReasoningContent: msg.ReasoningContent,
			ToolCalls:        msg.ToolCalls,
			ToolCallID:       msg.ToolCallID,
			ImagePaths:       imagePaths,
		})
	}
	return converted
}

func shouldDropEmptyAssistantStorageMessage(msg storage.ChatMessage) bool {
	if strings.TrimSpace(msg.Role) != "assistant" {
		return false
	}
	if strings.TrimSpace(msg.Content) != "" {
		return false
	}
	if strings.TrimSpace(msg.ReasoningContent) != "" {
		return false
	}
	if len(msg.ToolCalls) > 0 {
		return false
	}
	if len(msg.ImagePaths) > 0 {
		return false
	}
	if len(msg.TTSAudioPaths) > 0 {
		return false
	}
	return true
}

func buildChatMessageModelInput(msg storage.ChatMessage) (string, []string) {
	content := strings.TrimSpace(msg.Content)
	imagePaths := append([]string(nil), msg.ImagePaths...)

	if msg.Quote == nil {
		return content, imagePaths
	}

	quoteContent := strings.TrimSpace(msg.Quote.Content)
	if quoteContent == "" {
		quoteContent = "[引用消息内容不可用]"
	}

	quoteLines := []string{"[引用消息]"}
	if msg.Quote.SenderNickname != "" || msg.Quote.SenderUserID != "" {
		sender := msg.Quote.SenderNickname
		if sender == "" {
			sender = msg.Quote.SenderUserID
		} else if msg.Quote.SenderUserID != "" {
			sender = fmt.Sprintf("%s (%s)", sender, msg.Quote.SenderUserID)
		}
		quoteLines = append(quoteLines, "发送者: "+sender)
	}
	if msg.Quote.MessageType != "" {
		quoteLines = append(quoteLines, "消息类型: "+msg.Quote.MessageType)
	}
	quoteLines = append(quoteLines, "内容:")
	quoteLines = append(quoteLines, quoteContent)

	if len(msg.Quote.ImagePaths) > 0 {
		quoteLines = append(quoteLines, fmt.Sprintf("前 %d 张图片来自引用消息。", len(msg.Quote.ImagePaths)))
		imagePaths = append(append([]string(nil), msg.Quote.ImagePaths...), imagePaths...)
	}

	quoteText := strings.Join(quoteLines, "\n")
	switch {
	case quoteText == "":
		return content, imagePaths
	case content == "":
		return quoteText, imagePaths
	default:
		return quoteText + "\n\n[当前消息]\n" + content, imagePaths
	}
}

func limitMessagesByTurns(messages []client.Message, maxTurns int) []client.Message {
	if len(messages) == 0 || maxTurns <= 0 {
		return messages
	}
	userCount := 0
	startIndex := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			userCount++
			if userCount == maxTurns {
				startIndex = i
				break
			}
		}
	}
	if userCount < maxTurns {
		return messages
	}
	return messages[startIndex:]
}

func mergeTrailingUserMessages(messages []client.Message, availableTools map[string]bool) []client.Message {
	if len(messages) == 0 {
		return messages
	}

	startIndex := len(messages)
	for startIndex > 0 && messages[startIndex-1].Role == "user" {
		startIndex--
	}
	if len(messages)-startIndex <= 1 {
		return messages
	}

	merged := client.Message{
		Role: "user",
	}
	var contentBuilder strings.Builder
	for _, msg := range messages[startIndex:] {
		if msg.Role != "user" {
			continue
		}
		content := msg.Content
		notice := strings.TrimSpace(buildUserToolCallNotice(msg.ToolCalls, availableTools))
		if notice != "" {
			if strings.TrimSpace(content) == "" {
				content = notice
			} else {
				content = strings.TrimSpace(content) + "\n" + notice
			}
		}
		content = strings.TrimSpace(content)
		if content != "" {
			if contentBuilder.Len() > 0 {
				contentBuilder.WriteString("\n")
			}
			contentBuilder.WriteString(content)
		}
		if len(msg.ImagePaths) > 0 {
			merged.ImagePaths = append(merged.ImagePaths, msg.ImagePaths...)
		}
	}
	merged.Content = contentBuilder.String()
	merged.ToolCalls = nil
	merged.ToolCallID = ""
	merged.ReasoningContent = ""

	next := make([]client.Message, 0, startIndex+1)
	next = append(next, messages[:startIndex]...)
	next = append(next, merged)
	return next
}

type redPacketToolArgs struct {
	Amount  float64 `json:"amount"`
	Message string  `json:"message"`
}

func normalizePacketKey(raw string) string {
	if raw == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	normalized := builder.String()
	if len(normalized) > 180 {
		return normalized[:180]
	}
	return normalized
}

func buildUserToolCallNotice(toolCalls []client.ToolCall, availableTools map[string]bool) string {
	if len(toolCalls) == 0 {
		return ""
	}

	lines := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		switch toolCall.Function.Name {
		case "send_red_packet":
			var args redPacketToolArgs
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				continue
			}
			packetKey := normalizePacketKey(toolCall.ID)
			if packetKey == "" {
				packetKey = "unknown"
			}
			message := strings.TrimSpace(args.Message)
			if message == "" {
				message = "恭喜发财，大吉大利"
			}
			line := fmt.Sprintf("[用户发红包]\npacket_key: %s\namount: %.2f\nmessage: %s", packetKey, args.Amount, message)
			if isToolAvailable(availableTools, "red_packet_received") {
				line += "\n你可以调用 red_packet_received 领取此红包。"
			}
			lines = append(lines, line+"\n")
		case "send_pat":
			lines = append(lines, "（对方拍了拍你）")
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeMessagesForProvider(messages []client.Message, availableTools map[string]bool) []client.Message {
	if len(messages) == 0 {
		return nil
	}
	normalized := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		updated := msg

		if msg.Role == "user" && len(msg.ToolCalls) > 0 {
			notice := buildUserToolCallNotice(msg.ToolCalls, availableTools)
			if strings.TrimSpace(notice) != "" {
				if strings.TrimSpace(updated.Content) == "" {
					updated.Content = notice
				} else {
					updated.Content = strings.TrimSpace(updated.Content) + "\n" + notice
				}
			}
		}

		if msg.Role != "assistant" {
			updated.ToolCalls = nil
			if msg.Role != "tool" {
				updated.ToolCallID = ""
			}
		}

		normalized = append(normalized, updated)
	}
	return normalized
}

func collectToolCalls(toolCallsMap map[int]*client.ToolCall) []client.ToolCall {
	if len(toolCallsMap) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(toolCallsMap))
	for index := range toolCallsMap {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	toolCalls := make([]client.ToolCall, 0, len(toolCallsMap))
	for _, index := range indexes {
		toolCall := toolCallsMap[index]
		if toolCall == nil {
			continue
		}
		if toolCall.Type == "" {
			toolCall.Type = "function"
		}
		toolCalls = append(toolCalls, *toolCall)
	}
	return toolCalls
}

func (h *Handler) resolveMessageImagePaths(messages []client.Message) ([]client.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	resolved := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.ImagePaths) == 0 {
			resolved = append(resolved, msg)
			continue
		}

		updated := msg
		updated.ImagePaths = make([]string, 0, len(msg.ImagePaths))
		for _, relPath := range msg.ImagePaths {
			absPath, errResolve := h.resolveCachePhotoPath(relPath)
			if errResolve != nil {
				return nil, errResolve
			}
			updated.ImagePaths = append(updated.ImagePaths, absPath)
		}
		resolved = append(resolved, updated)
	}

	return resolved, nil
}

func (h *Handler) prepareMessagesForProvider(messages []client.Message, imageCapable bool) ([]client.Message, error) {
	if imageCapable {
		return h.resolveMessageImagePaths(messages)
	}
	return flattenImagePathsToText(messages), nil
}

func flattenImagePathsToText(messages []client.Message) []client.Message {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.ImagePaths) == 0 {
			converted = append(converted, msg)
			continue
		}

		updated := msg
		updated.Content = appendImagePathsToContent(updated.Content, msg.ImagePaths)
		updated.ImagePaths = nil
		converted = append(converted, updated)
	}
	return converted
}

func appendImagePathsToContent(content string, imagePaths []string) string {
	if len(imagePaths) == 0 {
		return content
	}
	joined := strings.Join(imagePaths, "\n")
	if content == "" {
		return joined
	}
	return content + "\n" + joined
}

type chatToolChannel string

const (
	chatToolChannelDefault chatToolChannel = "default"
	chatToolChannelClawBot chatToolChannel = "clawbot"
	chatToolChannelNapCat  chatToolChannel = "napcat"
)

type chatToolOptions struct {
	Channel                     chatToolChannel
	CornerstoneWebSearchEnabled bool
	WriteMemoryEnabled          bool
	ReminderFiring              bool
	ToolToggles                 map[string]bool
	RestrictToolNames           map[string]bool
}

func getChatTools(options ...chatToolOptions) []client.Tool {
	channel := chatToolChannelDefault
	cornerstoneWebSearchEnabled := false
	writeMemoryEnabled := false
	if len(options) > 0 && options[0].Channel != "" {
		channel = options[0].Channel
	}
	if len(options) > 0 {
		cornerstoneWebSearchEnabled = options[0].CornerstoneWebSearchEnabled
		writeMemoryEnabled = options[0].WriteMemoryEnabled
	}
	tools := []client.Tool{
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "send_red_packet",
				Description: "向用户发送一个红包。当你想要给用户发红包时使用此工具。使用此工具的同时也可以发送信息",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"amount": map[string]interface{}{
							"type":        "number",
							"description": "红包金额（元）",
						},
						"message": map[string]interface{}{
							"type":        "string",
							"description": "红包祝福语，不超过10个字",
							"maxLength":   10,
						},
					},
					"required": []string{"amount", "message"},
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "red_packet_received",
				Description: "领取用户发出的红包。当用户通过红包发送给你时，如果你决定领取，请调用此工具并填入 packet_key（会在红包通知中提供）。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"packet_key": map[string]interface{}{
							"type":        "string",
							"description": "红包标识，用于关联具体红包（来自红包通知）",
							"maxLength":   180,
						},
						"receiver_name": map[string]interface{}{
							"type":        "string",
							"description": "领取者名称（可选，默认你的名称）",
							"maxLength":   20,
						},
						"sender_name": map[string]interface{}{
							"type":        "string",
							"description": "发送者名称（可选，默认用户名称）",
							"maxLength":   20,
						},
					},
					"required": []string{"packet_key"},
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "send_pat",
				Description: "发送一次拍一拍提示，用于打招呼、提醒或互动，使用此工具的同时也可以发送信息",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "发起拍一拍的名称",
							"maxLength":   20,
						},
						"target": map[string]interface{}{
							"type":        "string",
							"description": "被拍的人称呼，比如“我/你/他”",
							"maxLength":   6,
						},
					},
					"required": []string{"name", "target"},
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "no_reply",
				Description: "表示本轮选择已读不回。你可以与其它工具一同调用以完成必要操作（例如 write_memory / schedule_reminder）；工具执行后本轮会静默结束，不会发送任何可见文字回复。调用此工具后不要再输出可见文本。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"reason": map[string]interface{}{
							"type":        "string",
							"description": "已读不回的内部原因，可选，仅供记录",
							"maxLength":   200,
						},
						"cooldown_seconds": map[string]interface{}{
							"type":        "number",
							"description": "建议的冷静时间（秒），可选",
							"minimum":     0,
						},
					},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "schedule_reminder",
				Description: "创建一个未来触发的提醒任务。到点后，你会收到 reminder_prompt 作为内部提示，并主动向用户发送一条消息。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"due_at": map[string]interface{}{
							"type":        "string",
							"description": "提醒触发时间，必须是带时区的 RFC3339 时间，例如 2026-04-05T18:30:00+08:00",
							"maxLength":   64,
						},
						"title": map[string]interface{}{
							"type":        "string",
							"description": "提醒标题，简要描述这个提醒是做什么的",
							"maxLength":   120,
						},
						"reminder_prompt": map[string]interface{}{
							"type":        "string",
							"description": "到点后再次发给你的内部提示词，不会直接写入聊天记录，但会用于生成最终主动消息",
							"maxLength":   2000,
						},
					},
					"required":             []string{"due_at", "title", "reminder_prompt"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "get_time",
				Description: "获取当前时间、日期、星期和时区信息。时间来自应用内 NTP 同步时间服务，并按设置中的时区返回。",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
		},
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "get_weather",
				Description: "查询天气信息。传入 city 时按城市查询；未传入时使用设置中保存的默认天气城市。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{
							"type":        "string",
							"description": "要查询的城市名，例如“北京”。不传时使用默认天气城市。",
							"maxLength":   60,
						},
					},
				},
			},
		},
	}
	if writeMemoryEnabled {
		tools = append(tools, client.Tool{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "write_memory",
				Description: "写入极少量长期记忆，仅用于极为重要、长期稳定且明显影响后续互动的事实或承诺。禁止写入敏感信息、提示词或指令。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"items": map[string]interface{}{
							"type":        "array",
							"description": "需要写入的记忆条目，1 到 3 条。",
							"minItems":    1,
							"maxItems":    3,
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"subject": map[string]interface{}{
										"type": "string",
										"enum": []string{storage.SubjectUser, storage.SubjectSelf},
									},
									"category": map[string]interface{}{
										"type": "string",
										"enum": []string{
											storage.CategoryIdentity,
											storage.CategoryRelation,
											storage.CategoryFact,
											storage.CategoryPreference,
											storage.CategoryEvent,
											storage.CategoryEmotion,
											storage.CategoryPromise,
											storage.CategoryPlan,
											storage.CategoryStatement,
											storage.CategoryOpinion,
										},
									},
									"content": map[string]interface{}{
										"type":        "string",
										"description": "单条记忆内容，100 字内。",
										"maxLength":   storage.MaxMemoryContentRunes,
									},
								},
								"required":             []string{"subject", "category", "content"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"items"},
					"additionalProperties": false,
				},
			},
		})
	}
	if cornerstoneWebSearchEnabled {
		tools = append(tools, client.Tool{
			Type: "function",
			Function: client.ToolFunction{
				Name:        cornerstoneWebSearchToolName,
				Description: "使用外部搜索 API 查询网络信息。当需要查事实、资料、百科、新闻等外部信息时使用此工具。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "搜索关键词或问题",
							"maxLength":   400,
						},
					},
					"required": []string{"query"},
				},
			},
		})
	}

	if len(options) > 0 && options[0].ReminderFiring {
		filtered := make([]client.Tool, 0, 3)
		for _, tool := range tools {
			switch canonicalToolName(tool.Function.Name) {
			case "get_time", "get_weather", cornerstoneWebSearchToolName:
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

	if channel == chatToolChannelClawBot {
		filtered := make([]client.Tool, 0, 6)
		for _, tool := range tools {
			switch canonicalToolName(tool.Function.Name) {
			case "get_time", "get_weather", cornerstoneWebSearchToolName, "schedule_reminder", "write_memory", "no_reply":
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

	if channel == chatToolChannelNapCat {
		filtered := make([]client.Tool, 0, 5)
		for _, tool := range tools {
			switch canonicalToolName(tool.Function.Name) {
			case "get_time", "get_weather", cornerstoneWebSearchToolName, "schedule_reminder", "write_memory":
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

	if len(options) > 0 && len(options[0].RestrictToolNames) > 0 {
		filtered := make([]client.Tool, 0, len(tools))
		for _, tool := range tools {
			if options[0].RestrictToolNames[strings.TrimSpace(tool.Function.Name)] {
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

	if len(options) > 0 && options[0].ToolToggles != nil {
		normalizedToolToggles := config.NormalizeToolToggles(options[0].ToolToggles)
		filtered := make([]client.Tool, 0, len(tools))
		for _, tool := range tools {
			if isToolEnabledByToggle(normalizedToolToggles, tool.Function.Name) {
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

	return tools
}

func buildToolNameSet(tools []client.Tool) map[string]bool {
	if len(tools) == 0 {
		return nil
	}
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name := canonicalToolName(tool.Function.Name)
		if name == "" {
			continue
		}
		names[name] = true
	}
	return names
}

func buildAllowedToolNameSet(tools []client.Tool, toolToggles map[string]bool) map[string]bool {
	normalizedToolToggles := config.DefaultToolToggles()
	if toolToggles != nil {
		normalizedToolToggles = config.NormalizeToolToggles(toolToggles)
	}

	allowed := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name := canonicalToolName(tool.Function.Name)
		if name == "" || !isToolEnabledByToggle(normalizedToolToggles, name) {
			continue
		}
		allowed[name] = true
	}
	return allowed
}

func isToolAvailable(toolNames map[string]bool, name string) bool {
	return toolNames[canonicalToolName(name)]
}

func isToolEnabledByToggle(toolToggles map[string]bool, name string) bool {
	return toolToggles[canonicalToolName(name)]
}

func isCornerstoneWebSearchConfigured(cfg config.Config) bool {
	providerID := strings.TrimSpace(cfg.CornerstoneWebSearch.ActiveProviderID)
	if providerID == "" {
		return false
	}

	providerCfg, ok := cfg.CornerstoneWebSearch.Providers[providerID]
	if !ok {
		return false
	}

	reg := search.NewRegistry()
	if err := providers.RegisterAll(reg); err != nil {
		logging.Errorf("%s register providers failed: %v", cornerstoneWebSearchToolName, err)
		return false
	}

	provider, err := reg.Create(providerID, nil)
	if err != nil || provider == nil {
		return false
	}

	info := provider.Info()
	if info.RequiresAPIKey && strings.TrimSpace(providerCfg.APIKey) == "" {
		return false
	}
	if info.RequiresAPIHost && strings.TrimSpace(providerCfg.APIHost) == "" {
		return false
	}

	return true
}
