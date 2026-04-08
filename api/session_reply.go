package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"fmt"
	"strings"
	"time"
)

type generatedSessionReply struct {
	Text            string
	NoReply         bool
	StorageMessages []storage.ChatMessage
	MemSession      *storage.MemorySession
}

type sessionReplyPersistMode string

const (
	sessionReplyPersistAll                sessionReplyPersistMode = "all"
	sessionReplyPersistFinalAssistantOnly sessionReplyPersistMode = "final_assistant_only"
)

type sessionReplyOptions struct {
	Session           *storage.ChatRecord
	PromptID          string
	PromptName        string
	Persona           string
	Channel           chatToolChannel
	Target            storage.ReminderTarget
	ToolOptions       chatToolOptions
	ExtraSystemGuides []string
	EphemeralMessages []client.Message
	PersistMode       sessionReplyPersistMode
}

func buildChatRequestForProvider(
	provider *config.Provider,
	messages []client.Message,
	tools []client.Tool,
	stream bool,
	maxTokens int,
	temperatureOverride *float64,
) client.ChatRequest {
	temperature := provider.Temperature
	if temperatureOverride != nil {
		temperature = *temperatureOverride
	}
	if provider.Type == config.ProviderTypeAnthropic {
		temperature = 1
	}

	req := client.ChatRequest{
		Model:       provider.Model,
		Messages:    messages,
		Stream:      stream,
		Temperature: temperature,
		TopP:        provider.TopP,
		MaxTokens:   maxTokens,
		Tools:       tools,
	}

	switch provider.Type {
	case config.ProviderTypeAnthropic:
		req.ThinkingBudget = provider.ThinkingBudget
		req.PromptCaching = provider.PromptCaching
		req.PromptCacheTTL = provider.PromptCacheTTL
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

	return req
}

func (h *Handler) generateSessionReply(ctx context.Context, options sessionReplyOptions) (*generatedSessionReply, error) {
	if h == nil {
		return nil, fmt.Errorf("handler is nil")
	}

	provider := h.configManager.GetActiveProvider()
	if provider == nil {
		return nil, fmt.Errorf("no active provider configured")
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		return nil, fmt.Errorf("active provider is not chat-capable")
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return nil, fmt.Errorf("api key not configured")
	}
	if options.Session == nil {
		return nil, fmt.Errorf("session is nil")
	}

	systemPrompt := h.configManager.GetSystemPrompt()
	userContext := buildChatUserContext(h.userManager.Get())

	promptID := strings.TrimSpace(options.PromptID)
	promptName := strings.TrimSpace(options.PromptName)
	persona := strings.TrimSpace(options.Persona)

	memSession := h.getOrCreateMemorySession(promptID, options.Session.SessionID)
	if memSession != nil {
		persona = storage.BuildPromptWithMemory(persona, memSession.GetActiveMemories())
	}

	currentConfig := config.DefaultConfig()
	if h.configManager != nil {
		currentConfig = h.configManager.Get()
	}

	effectiveToolOptions := options.ToolOptions
	if effectiveToolOptions.ToolToggles == nil {
		effectiveToolOptions.ToolToggles = currentConfig.ToolToggles
	}

	availableTools := getChatTools(effectiveToolOptions)
	availableToolNames := buildToolNameSet(availableTools)
	toolToggles := currentConfig.ToolToggles
	if effectiveToolOptions.ToolToggles != nil {
		toolToggles = effectiveToolOptions.ToolToggles
	}
	allowedToolNames := buildAllowedToolNameSet(availableTools, toolToggles)

	history := convertChatMessages(options.Session.Messages)
	history = mergeTrailingUserMessages(history, availableToolNames)
	history = limitMessagesByTurns(history, provider.ContextMessages)

	systemGuides := append([]string(nil), options.ExtraSystemGuides...)
	fullSystemPrompt := buildChatSystemPrompt(systemPrompt, userContext, persona, systemGuides...)

	messages := make([]client.Message, 0, len(history)+len(options.EphemeralMessages)+1)
	if strings.TrimSpace(fullSystemPrompt) != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: strings.TrimSpace(fullSystemPrompt),
		})
	}
	messages = append(messages, history...)
	if len(options.EphemeralMessages) > 0 {
		messages = append(messages, options.EphemeralMessages...)
	}
	messages = normalizeMessagesForProvider(messages, availableToolNames)

	resolvedMessages, errResolve := h.prepareMessagesForProvider(messages, provider.ImageCapable)
	if errResolve != nil {
		return nil, errResolve
	}

	req := buildChatRequestForProvider(provider, resolvedMessages, availableTools, false, 0, nil)

	toolExecutor := newChatToolExecutor()
	toolExecutor.memoryManager = h.memoryManager
	toolExecutor.configManager = h.configManager
	toolExecutor.weatherService = h.getWeatherService()
	toolExecutor.exactTimeService = h.exactTimeService
	toolExecutor.reminderService = h.reminderService
	if h.configManager != nil {
		toolExecutor.webSearch = newWebSearchOrchestrator(h.configManager.Get())
	}

	loopResult, err := runChatWithToolLoop(
		ctx,
		newAIClientForProvider(provider),
		req,
		toolExecutor,
		chatToolContext{
			SessionID:        options.Session.SessionID,
			PromptID:         promptID,
			PromptName:       promptName,
			Channel:          options.Channel,
			Target:           options.Target,
			MemSession:       memSession,
			AllowedToolNames: allowedToolNames,
		},
		nil,
	)
	if err != nil {
		return nil, err
	}
	text := ""
	if loopResult != nil && loopResult.FinalResponse != nil && len(loopResult.FinalResponse.Choices) > 0 {
		text = strings.TrimSpace(loopResult.FinalResponse.Choices[0].Message.Content)
	}
	noReplySelected := false
	if loopResult != nil {
		noReplySelected = containsNoReplyToolCall(loopResult.NewMessages)
	}

	storageMessages := buildGeneratedReplyStorageMessages(loopResult, text, options.PersistMode)

	return &generatedSessionReply{
		Text:            text,
		NoReply:         noReplySelected,
		StorageMessages: storageMessages,
		MemSession:      memSession,
	}, nil
}

func containsNoReplyToolCall(messages []client.Message) bool {
	for _, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		for _, toolCall := range msg.ToolCalls {
			if strings.TrimSpace(toolCall.Function.Name) == "no_reply" {
				return true
			}
		}
	}
	return false
}

func buildGeneratedReplyStorageMessages(
	loopResult *toolLoopResult,
	text string,
	persistMode sessionReplyPersistMode,
) []storage.ChatMessage {
	if persistMode == "" {
		persistMode = sessionReplyPersistAll
	}

	baseTime := time.Now()
	switch persistMode {
	case sessionReplyPersistFinalAssistantOnly:
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   text,
				Timestamp: baseTime,
			},
		}
	default:
		storageCapacity := 0
		if loopResult != nil {
			storageCapacity = len(loopResult.NewMessages)
		}
		storageMessages := make([]storage.ChatMessage, 0, storageCapacity)
		if loopResult == nil || len(loopResult.NewMessages) == 0 {
			return storageMessages
		}
		for index, msg := range loopResult.NewMessages {
			storageMessages = append(storageMessages, storage.ChatMessage{
				Role:             msg.Role,
				Content:          msg.Content,
				ReasoningContent: msg.ReasoningContent,
				ToolCalls:        msg.ToolCalls,
				ToolCallID:       msg.ToolCallID,
				ImagePaths:       msg.ImagePaths,
				TTSAudioPaths:    msg.TTSAudioPaths,
				Timestamp:        baseTime.Add(time.Millisecond * time.Duration(index)),
			})
		}
		return storageMessages
	}
}
