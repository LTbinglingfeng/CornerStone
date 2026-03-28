package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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
	userInfo := h.userManager.Get()
	var userContext string
	if userInfo.Username != "" || userInfo.Description != "" {
		userContext = "[用户信息]\n"
		if userInfo.Username != "" {
			userContext += fmt.Sprintf("用户名: %s\n", userInfo.Username)
		}
		if userInfo.Description != "" {
			userContext += fmt.Sprintf("用户自我描述: %s\n", userInfo.Description)
		}
	}

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

		now := time.Now()
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
		}
	}

	historyMessages := req.Messages
	if req.SaveHistory {
		if session, ok := h.chatManager.GetSession(sessionID); ok && len(session.Messages) > 0 {
			historyMessages = convertChatMessages(session.Messages)
		}
	}
	historyMessages = mergeTrailingUserMessages(historyMessages)
	historyMessages = limitMessagesByTurns(historyMessages, provider.ContextMessages)

	// 构建消息，顺序: 系统提示词 -> 用户信息 -> 人设
	messages := make([]client.Message, 0, len(historyMessages)+1)

	// 合并: 系统提示词在最前 + 用户信息在中间 + 人设在最后
	var fullSystemPrompt string
	if systemPrompt != "" {
		fullSystemPrompt = systemPrompt + "\n\n"
	}
	if userContext != "" {
		fullSystemPrompt += userContext + "\n"
	}
	if persona != "" {
		fullSystemPrompt += "[人设]\n" + persona
	}

	// 去除末尾多余换行
	fullSystemPrompt = strings.TrimSpace(fullSystemPrompt)

	redPacketGuide := strings.TrimSpace(`[红包交互]
当消息中出现 [用户发红包] 时，表示用户给你发了红包，并会提供 packet_key/amount/message。
如果你决定领取，请调用工具 red_packet_received 并传入 packet_key。`)

	if redPacketGuide != "" {
		if fullSystemPrompt == "" {
			fullSystemPrompt = redPacketGuide
		} else {
			fullSystemPrompt = strings.TrimSpace(fullSystemPrompt + "\n\n" + redPacketGuide)
		}
	}

	if fullSystemPrompt != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: fullSystemPrompt,
		})
	}
	messages = append(messages, historyMessages...)
	messages = normalizeMessagesForProvider(messages)
	resolvedMessages, errResolve := h.prepareMessagesForProvider(messages, provider.ImageCapable)
	if errResolve != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: errResolve.Error()})
		return
	}

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

	temperature := provider.Temperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}
	if provider.Type == config.ProviderTypeAnthropic {
		temperature = 1
	}

	chatReq := client.ChatRequest{
		Model:       provider.Model,
		Messages:    resolvedMessages,
		Stream:      useStream,
		Temperature: temperature,
		TopP:        provider.TopP,
		MaxTokens:   req.MaxTokens,
		Tools:       getChatTools(),
	}
	switch provider.Type {
	case config.ProviderTypeAnthropic:
		chatReq.ThinkingBudget = provider.ThinkingBudget
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
		chatReq.GeminiThinkingMode = geminiMode
		chatReq.GeminiThinkingLevel = geminiLevel
		chatReq.GeminiThinkingBudget = geminiBudget
	default:
		chatReq.ReasoningEffort = provider.ReasoningEffort
	}

	if useStream {
		h.handleStreamChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession, effectivePromptID, promptName)
	} else {
		h.handleNormalChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession, effectivePromptID, promptName)
	}
}

func (h *Handler) handleNormalChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession, promptID, promptName string) {
	ctxAI := context.WithoutCancel(r.Context())
	resp, err := aiClient.Chat(ctxAI, req)
	if err != nil {
		if r.Context().Err() != nil {
			logging.Errorf("chat request cancelled (client disconnected): %v", err)
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}

	// 保存AI回复到历史记录（包含思考内容）
	if len(resp.Choices) > 0 {
		message := resp.Choices[0].Message
		ttsAudioPaths := h.maybeGenerateTTSAudio(ctxAI, message.Content)
		if len(ttsAudioPaths) > 0 {
			resp.Choices[0].Message.TTSAudioPaths = ttsAudioPaths
		}
		if saveHistory {
			if errSaveHistory := h.chatManager.AddMessageWithDetails(sessionID, "assistant", message.Content, message.ReasoningContent, nil, message.ToolCalls, ttsAudioPaths); errSaveHistory != nil {
				if errors.Is(errSaveHistory, storage.ErrInvalidID) {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
					return
				}
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errSaveHistory.Error()})
				return
			}
		}
		h.handleMomentToolCalls(promptID, promptName, message.ToolCalls)
	}

	if r.Context().Err() != nil {
		return
	}

	if memSession != nil {
		memSession.OnRoundComplete()
	}

	// 返回响应，包含session_id
	result := map[string]interface{}{
		"session_id": sessionID,
		"response":   resp,
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})
}

// handleStreamChat 处理流式聊天 (SSE)
func (h *Handler) handleStreamChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession, promptID, promptName string) {
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

	var fullContent strings.Builder
	var fullReasoningContent strings.Builder
	toolCallsMap := make(map[int]*client.ToolCall)

	errChatStream := aiClient.ChatStream(ctxAI, req, func(chunk client.StreamChunk) error {
		// 收集完整内容
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				fullContent.WriteString(delta.Content)
			}
			// 收集思考内容
			if delta.ReasoningContent != "" {
				fullReasoningContent.WriteString(delta.ReasoningContent)
			}
			if len(delta.ToolCalls) > 0 {
				for _, toolCall := range delta.ToolCalls {
					existing, ok := toolCallsMap[toolCall.Index]
					if !ok {
						toolCallsMap[toolCall.Index] = &client.ToolCall{
							ID:   toolCall.ID,
							Type: toolCall.Type,
							Function: client.ToolCallFunction{
								Name:      toolCall.Function.Name,
								Arguments: toolCall.Function.Arguments,
							},
						}
						continue
					}
					if toolCall.ID != "" {
						existing.ID = toolCall.ID
					}
					if toolCall.Type != "" {
						existing.Type = toolCall.Type
					}
					if toolCall.Function.Name != "" {
						existing.Function.Name = toolCall.Function.Name
					}
					if toolCall.Function.Arguments != "" {
						existing.Function.Arguments += toolCall.Function.Arguments
					}
				}
			}
		}

		if isClientDisconnected() {
			return nil
		}

		data, errMarshal := json.Marshal(chunk)
		if errMarshal != nil {
			logging.Errorf("marshal stream chunk error: %v", errMarshal)
			clientDisconnected = true
			return nil
		}
		if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", data); errWrite != nil {
			clientDisconnected = true
			return nil
		}
		flusher.Flush()
		return nil
	})

	if errChatStream != nil {
		logging.Errorf("Stream error: %v", errChatStream)
		if !isClientDisconnected() {
			errorPayload, _ := json.Marshal(map[string]string{"error": errChatStream.Error()})
			if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", errorPayload); errWrite != nil {
				clientDisconnected = true
			} else {
				flusher.Flush()
			}
		}
	}

	// 保存AI回复到历史记录（包含思考内容）
	toolCalls := collectToolCalls(toolCallsMap)
	h.handleMomentToolCalls(promptID, promptName, toolCalls)
	ttsAudioPaths := []string(nil)
	if errChatStream == nil && fullContent.Len() > 0 {
		ttsAudioPaths = h.maybeGenerateTTSAudio(ctxAI, fullContent.String())
	}
	if saveHistory && (fullContent.Len() > 0 || fullReasoningContent.Len() > 0 || len(toolCalls) > 0) {
		if errSaveHistory := h.chatManager.AddMessageWithDetails(sessionID, "assistant", fullContent.String(), fullReasoningContent.String(), nil, toolCalls, ttsAudioPaths); errSaveHistory != nil {
			logging.Errorf("save stream history error: %v", errSaveHistory)
			errorMessage := errSaveHistory.Error()
			if errors.Is(errSaveHistory, storage.ErrInvalidID) {
				errorMessage = "Invalid session ID"
			}
			if !isClientDisconnected() {
				errorPayload, _ := json.Marshal(map[string]string{"error": errorMessage})
				if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", errorPayload); errWrite != nil {
					clientDisconnected = true
				} else {
					flusher.Flush()
				}
			}
		}
	}

	if errChatStream == nil && len(ttsAudioPaths) > 0 && !isClientDisconnected() {
		ttsPayload, _ := json.Marshal(map[string]interface{}{
			"tts_audio_paths": ttsAudioPaths,
		})
		if _, errWrite := fmt.Fprintf(w, "data: %s\n\n", ttsPayload); errWrite != nil {
			clientDisconnected = true
		} else {
			flusher.Flush()
		}
	}

	if errChatStream == nil && memSession != nil {
		memSession.OnRoundComplete()
	}

	if !isClientDisconnected() {
		if _, errWrite := fmt.Fprintf(w, "data: [DONE]\n\n"); errWrite != nil {
			clientDisconnected = true
		} else {
			flusher.Flush()
		}
	}
}

type generateMomentToolArgs struct {
	Content       string `json:"content"`
	ImagePrompt   string `json:"image_prompt"`
	Prompt        string `json:"prompt"`
	ImagePromptV2 string `json:"imagePrompt"`
}

func (h *Handler) handleMomentToolCalls(promptID, promptName string, toolCalls []client.ToolCall) {
	if h.momentManager == nil || h.momentGenerator == nil {
		return
	}
	promptID = strings.TrimSpace(promptID)
	promptName = strings.TrimSpace(promptName)
	if promptID == "" || promptName == "" || len(toolCalls) == 0 {
		return
	}

	for _, tc := range toolCalls {
		if tc.Function.Name != "generate_moment" {
			continue
		}

		var args generateMomentToolArgs
		if errUnmarshal := json.Unmarshal([]byte(tc.Function.Arguments), &args); errUnmarshal != nil {
			logging.Warnf("invalid generate_moment args: %v", errUnmarshal)
			continue
		}

		args.Content = strings.TrimSpace(args.Content)
		imagePrompt := strings.TrimSpace(args.ImagePrompt)
		if imagePrompt == "" {
			imagePrompt = strings.TrimSpace(args.Prompt)
		}
		if imagePrompt == "" {
			imagePrompt = strings.TrimSpace(args.ImagePromptV2)
		}
		if args.Content == "" || imagePrompt == "" {
			logging.Warnf("generate_moment missing fields: prompt_id=%s args=%s", promptID, logging.Truncate(tc.Function.Arguments, 200))
			continue
		}

		now := time.Now()
		moment := storage.Moment{
			ID:          uuid.NewString(),
			PromptID:    promptID,
			PromptName:  promptName,
			Content:     args.Content,
			ImagePrompt: imagePrompt,
			Status:      storage.MomentStatusPending,
			CreatedAt:   now,
			UpdatedAt:   now,
			Likes:       []storage.Like{},
			Comments:    []storage.Comment{},
		}

		created, errCreate := h.momentManager.Create(moment)
		if errCreate != nil {
			logging.Errorf("failed to create moment: %v", errCreate)
			continue
		}

		h.momentGenerator.StartGeneration(created.ID)
		logging.Infof("moment created via tool call: id=%s prompt_id=%s", created.ID, promptID)
	}
}

func convertChatMessages(messages []storage.ChatMessage) []client.Message {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		converted = append(converted, client.Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCalls:        msg.ToolCalls,
			ImagePaths:       msg.ImagePaths,
		})
	}
	return converted
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

func mergeTrailingUserMessages(messages []client.Message) []client.Message {
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
		notice := strings.TrimSpace(buildUserToolCallNotice(msg.ToolCalls))
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

func buildUserToolCallNotice(toolCalls []client.ToolCall) string {
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
			lines = append(lines, fmt.Sprintf("[用户发红包]\npacket_key: %s\namount: %.2f\nmessage: %s\n你可以调用 red_packet_received 领取此红包。\n", packetKey, args.Amount, message))
		case "send_pat":
			lines = append(lines, "（对方拍了拍你）")
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeMessagesForProvider(messages []client.Message) []client.Message {
	if len(messages) == 0 {
		return nil
	}
	normalized := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		updated := msg

		if msg.Role == "user" && len(msg.ToolCalls) > 0 {
			notice := buildUserToolCallNotice(msg.ToolCalls)
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
)

type chatToolOptions struct {
	Channel chatToolChannel
}

func getChatTools(options ...chatToolOptions) []client.Tool {
	channel := chatToolChannelDefault
	if len(options) > 0 && options[0].Channel != "" {
		channel = options[0].Channel
	}
	if channel == chatToolChannelClawBot {
		return nil
	}
	return []client.Tool{
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
				Name:        "generate_moment",
				Description: "发布一条朋友圈动态。当你想分享生活、心情或有趣的事情时使用此工具。需要提供朋友圈文案和用于生成配图的提示词。",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content": map[string]interface{}{
							"type":        "string",
							"description": "朋友圈文案内容，表达你想分享的内容",
							"maxLength":   500,
						},
						"prompt": map[string]interface{}{
							"type":        "string",
							"description": "生成配图的英文提示词（也可以使用 image_prompt 字段）",
							"maxLength":   1000,
						},
						"image_prompt": map[string]interface{}{
							"type":        "string",
							"description": "生成配图的英文提示词，描述你想要的图片内容和风格",
							"maxLength":   1000,
						},
					},
					"required": []string{"content", "prompt"},
				},
			},
		},
	}
}
