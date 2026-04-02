package api

import (
	"cornerstone/config"
	"net/http"
	"strings"
)

func defaultProviderBaseURL(providerType config.ProviderType) string {
	switch providerType {
	case config.ProviderTypeGemini, config.ProviderTypeGeminiImage:
		return "https://generativelanguage.googleapis.com/v1beta"
	case config.ProviderTypeAnthropic:
		return "https://api.anthropic.com/v1"
	case config.ProviderTypeOpenAI, config.ProviderTypeOpenAIResponse:
		fallthrough
	default:
		return "https://api.openai.com/v1"
	}
}

func normalizeProviderBaseURL(providerType config.ProviderType, baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL != "" {
		return baseURL
	}
	return defaultProviderBaseURL(providerType)
}

// ConfigUpdateRequest 配置更新请求
type ConfigUpdateRequest struct {
	BaseURL                *string `json:"base_url,omitempty"`
	APIKey                 *string `json:"api_key,omitempty"`
	Model                  *string `json:"model,omitempty"`
	SystemPrompt           *string `json:"system_prompt,omitempty"`
	ReplyWaitWindowMode    *string `json:"reply_wait_window_mode,omitempty"`
	ReplyWaitWindowSeconds *int    `json:"reply_wait_window_seconds,omitempty"`
}

// ProviderRequest 供应商请求
type ProviderRequest struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Type                      string   `json:"type"` // 供应商类型 (openai/openai_response/gemini/gemini_image/anthropic)
	BaseURL                   string   `json:"base_url"`
	APIKey                    string   `json:"api_key"`
	Model                     string   `json:"model"`
	Temperature               *float64 `json:"temperature,omitempty"`
	TopP                      *float64 `json:"top_p,omitempty"`
	ThinkingBudget            *int     `json:"thinking_budget,omitempty"`
	PromptCaching             *bool    `json:"prompt_caching,omitempty"`
	PromptCacheTTL            *string  `json:"prompt_cache_ttl,omitempty"`
	ReasoningEffort           *string  `json:"reasoning_effort,omitempty"`
	GeminiThinkingMode        *string  `json:"gemini_thinking_mode,omitempty"`
	GeminiThinkingLevel       *string  `json:"gemini_thinking_level,omitempty"`
	GeminiThinkingBudget      *int     `json:"gemini_thinking_budget,omitempty"`
	GeminiImageAspectRatio    *string  `json:"gemini_image_aspect_ratio,omitempty"`
	GeminiImageSize           *string  `json:"gemini_image_size,omitempty"`
	GeminiImageNumberOfImages *int     `json:"gemini_image_number_of_images,omitempty"`
	GeminiImageOutputMIMEType *string  `json:"gemini_image_output_mime_type,omitempty"`
	ContextMessages           *int     `json:"context_messages,omitempty"`
	Stream                    bool     `json:"stream"`        // 是否启用流式输出
	ImageCapable              bool     `json:"image_capable"` // 是否支持识图
}

// SetActiveProviderRequest 设置激活供应商请求
type SetActiveProviderRequest struct {
	ProviderID string `json:"provider_id"`
}

type MemoryProviderConfigRequest struct {
	UseCustom bool             `json:"use_custom"`
	Provider  *ProviderRequest `json:"provider,omitempty"`
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		provider := h.configManager.GetActiveProvider()
		if provider == nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "No active provider"})
			return
		}
		// 返回兼容旧格式的配置
		cfg := struct {
			BaseURL                string `json:"base_url"`
			APIKey                 string `json:"api_key"`
			Model                  string `json:"model"`
			SystemPrompt           string `json:"system_prompt"`
			ReplyWaitWindowMode    string `json:"reply_wait_window_mode"`
			ReplyWaitWindowSeconds int    `json:"reply_wait_window_seconds"`
		}{
			BaseURL:                provider.BaseURL,
			APIKey:                 provider.APIKey,
			Model:                  provider.Model,
			SystemPrompt:           h.configManager.GetSystemPrompt(),
			ReplyWaitWindowMode:    h.configManager.Get().ReplyWaitWindowMode,
			ReplyWaitWindowSeconds: h.configManager.Get().ReplyWaitWindowSeconds,
		}
		// 隐藏完整API密钥
		if len(cfg.APIKey) > 8 {
			cfg.APIKey = cfg.APIKey[:4] + "****" + cfg.APIKey[len(cfg.APIKey)-4:]
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: cfg})

	case "PUT", "POST":
		var req ConfigUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		updates := make(map[string]string)
		if req.BaseURL != nil {
			updates["base_url"] = *req.BaseURL
		}
		if req.APIKey != nil {
			updates["api_key"] = *req.APIKey
		}
		if req.Model != nil {
			updates["model"] = *req.Model
		}
		if req.SystemPrompt != nil {
			updates["system_prompt"] = *req.SystemPrompt
		}

		if err := h.configManager.UpdatePartial(updates); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		if req.ReplyWaitWindowMode != nil || req.ReplyWaitWindowSeconds != nil {
			cfg := h.configManager.Get()
			if req.ReplyWaitWindowMode != nil {
				cfg.ReplyWaitWindowMode = *req.ReplyWaitWindowMode
			}
			if req.ReplyWaitWindowSeconds != nil {
				cfg.ReplyWaitWindowSeconds = *req.ReplyWaitWindowSeconds
			}
			if err := h.configManager.Update(cfg); err != nil {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
				return
			}
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Configuration updated"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleProviders 处理供应商列表请求
func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		providers := h.configManager.GetProviders()
		activeID := h.configManager.GetActiveProviderID()
		cfg := h.configManager.Get()
		// 隐藏API密钥
		for i := range providers {
			if providers[i].Type == config.ProviderTypeAnthropic {
				providers[i].Temperature = 1
				providers[i].TopP = 0
			}
			if len(providers[i].APIKey) > 8 {
				providers[i].APIKey = providers[i].APIKey[:4] + "****" + providers[i].APIKey[len(providers[i].APIKey)-4:]
			} else if len(providers[i].APIKey) > 0 {
				providers[i].APIKey = "****"
			}
		}

		var memoryProvider *config.Provider
		if cfg.MemoryProvider != nil {
			clone := *cfg.MemoryProvider
			if clone.Type == config.ProviderTypeAnthropic {
				clone.Temperature = 1
				clone.TopP = 0
			}
			if len(clone.APIKey) > 8 {
				clone.APIKey = clone.APIKey[:4] + "****" + clone.APIKey[len(clone.APIKey)-4:]
			} else if len(clone.APIKey) > 0 {
				clone.APIKey = "****"
			}
			memoryProvider = &clone
		}
		result := map[string]interface{}{
			"providers":                 providers,
			"active_provider_id":        activeID,
			"system_prompt":             h.configManager.GetSystemPrompt(),
			"reply_wait_window_mode":    cfg.ReplyWaitWindowMode,
			"reply_wait_window_seconds": cfg.ReplyWaitWindowSeconds,
			"image_provider_id":         cfg.ImageProviderID,
			"memory_provider_id":        cfg.MemoryProviderID,
			"memory_provider":           memoryProvider,
			"memory_enabled":            cfg.MemoryEnabled,
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})

	case "POST":
		var req ProviderRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		req.ID = strings.TrimSpace(req.ID)
		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimSpace(req.BaseURL)
		req.APIKey = strings.TrimSpace(req.APIKey)
		req.Model = strings.TrimSpace(req.Model)

		if req.ID == "" || req.Name == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "ID and name are required"})
			return
		}

		// 设置默认类型为 openai
		providerType := config.ProviderType(req.Type)
		if providerType == "" {
			providerType = config.ProviderTypeOpenAI
		}

		defaultProvider := config.DefaultProvider()
		temperature := defaultProvider.Temperature
		topP := defaultProvider.TopP
		thinkingBudget := defaultProvider.ThinkingBudget
		promptCaching := defaultProvider.PromptCaching
		promptCacheTTL := defaultProvider.PromptCacheTTL
		reasoningEffort := defaultProvider.ReasoningEffort
		contextMessages := defaultProvider.ContextMessages
		if req.Temperature != nil {
			temperature = *req.Temperature
		}
		if req.TopP != nil {
			topP = *req.TopP
		}
		if req.ThinkingBudget != nil {
			thinkingBudget = *req.ThinkingBudget
		}
		if req.PromptCaching != nil {
			promptCaching = *req.PromptCaching
		}
		if req.PromptCacheTTL != nil {
			promptCacheTTL = normalizeAnthropicPromptCacheTTL(*req.PromptCacheTTL)
		}
		if req.ReasoningEffort != nil {
			reasoningEffort = *req.ReasoningEffort
		}
		if req.ContextMessages != nil {
			contextMessages = *req.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
			topP = 0
		}

		baseURL := normalizeProviderBaseURL(providerType, req.BaseURL)
		if req.APIKey == "" || req.Model == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "API key and model are required"})
			return
		}

		var geminiThinkingMode *string
		var geminiThinkingLevel *string
		var geminiThinkingBudget *int
		if providerType == config.ProviderTypeGemini {
			mode := "none"
			level := "low"
			budget := 128
			if req.GeminiThinkingMode != nil {
				mode = strings.TrimSpace(*req.GeminiThinkingMode)
			}
			if req.GeminiThinkingLevel != nil {
				level = strings.TrimSpace(*req.GeminiThinkingLevel)
			}
			if req.GeminiThinkingBudget != nil {
				budget = *req.GeminiThinkingBudget
			}
			geminiThinkingMode = &mode
			geminiThinkingLevel = &level
			geminiThinkingBudget = &budget
		}

		var geminiImageAspectRatio *string
		var geminiImageSize *string
		var geminiImageNumberOfImages *int
		var geminiImageOutputMIMEType *string
		if providerType == config.ProviderTypeGeminiImage {
			aspectRatio := normalizeGeminiImageAspectRatio("")
			if req.GeminiImageAspectRatio != nil {
				aspectRatio = normalizeGeminiImageAspectRatio(*req.GeminiImageAspectRatio)
			}
			geminiImageAspectRatio = &aspectRatio

			size := ""
			if req.GeminiImageSize != nil {
				size = normalizeGeminiImageSize(*req.GeminiImageSize)
			}
			if size != "" {
				geminiImageSize = &size
			}

			numberOfImages := 1
			if req.GeminiImageNumberOfImages != nil {
				numberOfImages = *req.GeminiImageNumberOfImages
			}
			numberOfImages = clampGeminiImageNumberOfImages(numberOfImages)
			geminiImageNumberOfImages = &numberOfImages

			outputMIMEType := "image/jpeg"
			if req.GeminiImageOutputMIMEType != nil {
				outputMIMEType = *req.GeminiImageOutputMIMEType
			}
			outputMIMEType = normalizeGeminiImageOutputMIMEType(outputMIMEType)
			geminiImageOutputMIMEType = &outputMIMEType
		}

		provider := config.Provider{
			ID:                        req.ID,
			Name:                      req.Name,
			Type:                      providerType,
			BaseURL:                   baseURL,
			APIKey:                    req.APIKey,
			Model:                     req.Model,
			Temperature:               temperature,
			TopP:                      topP,
			ThinkingBudget:            thinkingBudget,
			PromptCaching:             promptCaching,
			PromptCacheTTL:            promptCacheTTL,
			ReasoningEffort:           reasoningEffort,
			GeminiThinkingMode:        geminiThinkingMode,
			GeminiThinkingLevel:       geminiThinkingLevel,
			GeminiThinkingBudget:      geminiThinkingBudget,
			GeminiImageAspectRatio:    geminiImageAspectRatio,
			GeminiImageSize:           geminiImageSize,
			GeminiImageNumberOfImages: geminiImageNumberOfImages,
			GeminiImageOutputMIMEType: geminiImageOutputMIMEType,
			ContextMessages:           contextMessages,
			Stream:                    req.Stream,
			ImageCapable:              req.ImageCapable,
		}

		if err := h.configManager.AddProvider(provider); err != nil {
			if err == config.ErrProviderExists {
				h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: err.Error()})
			} else {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			}
			return
		}

		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: provider})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleProviderByID 处理单个供应商请求
func (h *Handler) handleProviderByID(w http.ResponseWriter, r *http.Request) {
	// 检查是否是 /management/providers/active 路径
	if strings.HasSuffix(r.URL.Path, "/active") {
		h.handleActiveProvider(w, r)
		return
	}

	// 检查是否是 /management/providers/{id}/models 路径
	if strings.HasSuffix(r.URL.Path, "/models") {
		h.handleProviderModels(w, r)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/management/providers/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider ID required"})
		return
	}

	switch r.Method {
	case "GET":
		provider := h.configManager.GetProvider(id)
		if provider == nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Provider not found"})
			return
		}
		if provider.Type == config.ProviderTypeAnthropic {
			provider.Temperature = 1
		}
		// 隐藏API密钥
		if len(provider.APIKey) > 8 {
			provider.APIKey = provider.APIKey[:4] + "****" + provider.APIKey[len(provider.APIKey)-4:]
		} else if len(provider.APIKey) > 0 {
			provider.APIKey = "****"
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: provider})

	case "PUT":
		var req ProviderRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimSpace(req.BaseURL)
		req.APIKey = strings.TrimSpace(req.APIKey)
		req.Model = strings.TrimSpace(req.Model)

		if req.Name == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Name is required"})
			return
		}

		// 设置默认类型为 openai
		providerType := config.ProviderType(req.Type)
		if providerType == "" {
			providerType = config.ProviderTypeOpenAI
		}

		defaultProvider := config.DefaultProvider()
		existingProvider := h.configManager.GetProvider(id)
		apiKey := req.APIKey
		if existingProvider != nil && (apiKey == "" || strings.Contains(apiKey, "*")) {
			apiKey = existingProvider.APIKey
		}
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "API key is required"})
			return
		}

		baseURL := req.BaseURL
		if baseURL == "" && existingProvider != nil {
			baseURL = strings.TrimSpace(existingProvider.BaseURL)
		}
		baseURL = normalizeProviderBaseURL(providerType, baseURL)

		model := req.Model
		if model == "" && existingProvider != nil {
			model = strings.TrimSpace(existingProvider.Model)
		}
		model = strings.TrimSpace(model)
		if model == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Model is required"})
			return
		}

		temperature := defaultProvider.Temperature
		topP := defaultProvider.TopP
		thinkingBudget := defaultProvider.ThinkingBudget
		promptCaching := defaultProvider.PromptCaching
		promptCacheTTL := defaultProvider.PromptCacheTTL
		reasoningEffort := defaultProvider.ReasoningEffort
		contextMessages := defaultProvider.ContextMessages

		geminiMode := "none"
		geminiLevel := "low"
		geminiBudget := 128
		geminiImageAspectRatioValue := "1:1"
		geminiImageSizeValue := ""
		geminiImageNumberOfImagesValue := 1
		geminiImageOutputMIMETypeValue := "image/jpeg"
		if existingProvider != nil {
			temperature = existingProvider.Temperature
			topP = existingProvider.TopP
			thinkingBudget = existingProvider.ThinkingBudget
			promptCaching = existingProvider.PromptCaching
			promptCacheTTL = normalizeAnthropicPromptCacheTTL(existingProvider.PromptCacheTTL)
			reasoningEffort = existingProvider.ReasoningEffort
			if existingProvider.Type == config.ProviderTypeGemini {
				if existingProvider.GeminiThinkingMode != nil {
					geminiMode = *existingProvider.GeminiThinkingMode
				}
				if existingProvider.GeminiThinkingLevel != nil {
					geminiLevel = *existingProvider.GeminiThinkingLevel
				}
				if existingProvider.GeminiThinkingBudget != nil {
					geminiBudget = *existingProvider.GeminiThinkingBudget
				}
			}
			if existingProvider.Type == config.ProviderTypeGeminiImage {
				if existingProvider.GeminiImageAspectRatio != nil {
					geminiImageAspectRatioValue = *existingProvider.GeminiImageAspectRatio
				}
				if existingProvider.GeminiImageSize != nil {
					geminiImageSizeValue = *existingProvider.GeminiImageSize
				}
				if existingProvider.GeminiImageNumberOfImages != nil {
					geminiImageNumberOfImagesValue = *existingProvider.GeminiImageNumberOfImages
				}
				if existingProvider.GeminiImageOutputMIMEType != nil {
					geminiImageOutputMIMETypeValue = *existingProvider.GeminiImageOutputMIMEType
				}
			}
			contextMessages = existingProvider.ContextMessages
		}
		if req.Temperature != nil {
			temperature = *req.Temperature
		}
		if req.TopP != nil {
			topP = *req.TopP
		}
		if req.ThinkingBudget != nil {
			thinkingBudget = *req.ThinkingBudget
		}
		if req.PromptCaching != nil {
			promptCaching = *req.PromptCaching
		}
		if req.PromptCacheTTL != nil {
			promptCacheTTL = normalizeAnthropicPromptCacheTTL(*req.PromptCacheTTL)
		}
		if req.ReasoningEffort != nil {
			reasoningEffort = *req.ReasoningEffort
		}
		if req.ContextMessages != nil {
			contextMessages = *req.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
			topP = 0
		}

		var geminiThinkingMode *string
		var geminiThinkingLevel *string
		var geminiThinkingBudget *int
		if providerType == config.ProviderTypeGemini {
			if req.GeminiThinkingMode != nil {
				geminiMode = strings.TrimSpace(*req.GeminiThinkingMode)
			}
			if req.GeminiThinkingLevel != nil {
				geminiLevel = strings.TrimSpace(*req.GeminiThinkingLevel)
			}
			if req.GeminiThinkingBudget != nil {
				geminiBudget = *req.GeminiThinkingBudget
			}
			geminiThinkingMode = &geminiMode
			geminiThinkingLevel = &geminiLevel
			geminiThinkingBudget = &geminiBudget
		}

		var geminiImageAspectRatio *string
		var geminiImageSize *string
		var geminiImageNumberOfImages *int
		var geminiImageOutputMIMEType *string
		if providerType == config.ProviderTypeGeminiImage {
			if req.GeminiImageAspectRatio != nil {
				geminiImageAspectRatioValue = *req.GeminiImageAspectRatio
			}
			geminiImageAspectRatioValue = normalizeGeminiImageAspectRatio(geminiImageAspectRatioValue)
			geminiImageAspectRatio = &geminiImageAspectRatioValue

			if req.GeminiImageSize != nil {
				geminiImageSizeValue = *req.GeminiImageSize
			}
			geminiImageSizeValue = normalizeGeminiImageSize(geminiImageSizeValue)
			if geminiImageSizeValue != "" {
				geminiImageSize = &geminiImageSizeValue
			}

			if req.GeminiImageNumberOfImages != nil {
				geminiImageNumberOfImagesValue = *req.GeminiImageNumberOfImages
			}
			geminiImageNumberOfImagesValue = clampGeminiImageNumberOfImages(geminiImageNumberOfImagesValue)
			geminiImageNumberOfImages = &geminiImageNumberOfImagesValue

			if req.GeminiImageOutputMIMEType != nil {
				geminiImageOutputMIMETypeValue = *req.GeminiImageOutputMIMEType
			}
			geminiImageOutputMIMETypeValue = normalizeGeminiImageOutputMIMEType(geminiImageOutputMIMETypeValue)
			geminiImageOutputMIMEType = &geminiImageOutputMIMETypeValue
		}

		provider := config.Provider{
			ID:                        id,
			Name:                      req.Name,
			Type:                      providerType,
			BaseURL:                   baseURL,
			APIKey:                    apiKey,
			Model:                     model,
			Temperature:               temperature,
			TopP:                      topP,
			ThinkingBudget:            thinkingBudget,
			PromptCaching:             promptCaching,
			PromptCacheTTL:            promptCacheTTL,
			ReasoningEffort:           reasoningEffort,
			GeminiThinkingMode:        geminiThinkingMode,
			GeminiThinkingLevel:       geminiThinkingLevel,
			GeminiThinkingBudget:      geminiThinkingBudget,
			GeminiImageAspectRatio:    geminiImageAspectRatio,
			GeminiImageSize:           geminiImageSize,
			GeminiImageNumberOfImages: geminiImageNumberOfImages,
			GeminiImageOutputMIMEType: geminiImageOutputMIMEType,
			ContextMessages:           contextMessages,
			Stream:                    req.Stream,
			ImageCapable:              req.ImageCapable,
		}

		if err := h.configManager.UpdateProvider(provider); err != nil {
			if err == config.ErrProviderNotFound {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: err.Error()})
			} else if err == config.ErrProviderNotChatCapable {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
			} else {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			}
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: provider})

	case "DELETE":
		if err := h.configManager.DeleteProvider(id); err != nil {
			if err == config.ErrProviderNotFound {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: err.Error()})
			} else if err == config.ErrCannotDeleteLastProvider {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
			} else {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			}
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Provider deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleActiveProvider 处理激活供应商请求
func (h *Handler) handleActiveProvider(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		provider := h.configManager.GetActiveProvider()
		if provider == nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "No active provider"})
			return
		}
		if provider.Type == config.ProviderTypeAnthropic {
			provider.Temperature = 1
		}
		// 隐藏API密钥
		if len(provider.APIKey) > 8 {
			provider.APIKey = provider.APIKey[:4] + "****" + provider.APIKey[len(provider.APIKey)-4:]
		} else if len(provider.APIKey) > 0 {
			provider.APIKey = "****"
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: provider})

	case "PUT", "POST":
		var req SetActiveProviderRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if err := h.configManager.SetActiveProvider(req.ProviderID); err != nil {
			if err == config.ErrProviderNotFound {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: err.Error()})
			} else if err == config.ErrProviderNotChatCapable {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
			} else {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			}
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Active provider updated"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleMemoryProvider(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.configManager.Get()
		var memoryProvider *config.Provider
		if cfg.MemoryProvider != nil {
			clone := *cfg.MemoryProvider
			if clone.Type == config.ProviderTypeAnthropic {
				clone.Temperature = 1
			}
			if len(clone.APIKey) > 8 {
				clone.APIKey = clone.APIKey[:4] + "****" + clone.APIKey[len(clone.APIKey)-4:]
			} else if len(clone.APIKey) > 0 {
				clone.APIKey = "****"
			}
			memoryProvider = &clone
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_provider": memoryProvider}})

	case http.MethodPut:
		var req MemoryProviderConfigRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		cfg := h.configManager.Get()
		if !req.UseCustom {
			cfg.MemoryProvider = nil
			cfg.MemoryProviderID = ""
			if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
				h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
				return
			}
			h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_provider": nil}})
			return
		}

		if req.Provider == nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider required"})
			return
		}

		providerType := config.ProviderType(req.Provider.Type)
		if providerType == "" {
			providerType = config.ProviderTypeOpenAI
		}
		if providerType == config.ProviderTypeGeminiImage {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider is not chat-capable"})
			return
		}

		existingProvider := cfg.MemoryProvider
		apiKey := strings.TrimSpace(req.Provider.APIKey)
		if existingProvider != nil && (apiKey == "" || strings.Contains(apiKey, "*")) {
			apiKey = existingProvider.APIKey
		}
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "API key is required"})
			return
		}

		baseURL := strings.TrimSpace(req.Provider.BaseURL)
		if baseURL == "" && existingProvider != nil {
			baseURL = strings.TrimSpace(existingProvider.BaseURL)
		}
		baseURL = normalizeProviderBaseURL(providerType, baseURL)

		model := strings.TrimSpace(req.Provider.Model)
		if model == "" && existingProvider != nil {
			model = strings.TrimSpace(existingProvider.Model)
		}
		if model == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Model is required"})
			return
		}

		defaultProvider := config.DefaultProvider()
		temperature := defaultProvider.Temperature
		topP := defaultProvider.TopP
		thinkingBudget := defaultProvider.ThinkingBudget
		promptCaching := defaultProvider.PromptCaching
		promptCacheTTL := defaultProvider.PromptCacheTTL
		reasoningEffort := defaultProvider.ReasoningEffort
		contextMessages := defaultProvider.ContextMessages

		geminiMode := "none"
		geminiLevel := "low"
		geminiBudget := 128

		if existingProvider != nil {
			temperature = existingProvider.Temperature
			topP = existingProvider.TopP
			thinkingBudget = existingProvider.ThinkingBudget
			promptCaching = existingProvider.PromptCaching
			promptCacheTTL = normalizeAnthropicPromptCacheTTL(existingProvider.PromptCacheTTL)
			reasoningEffort = existingProvider.ReasoningEffort
			contextMessages = existingProvider.ContextMessages

			if existingProvider.Type == config.ProviderTypeGemini {
				if existingProvider.GeminiThinkingMode != nil {
					geminiMode = *existingProvider.GeminiThinkingMode
				}
				if existingProvider.GeminiThinkingLevel != nil {
					geminiLevel = *existingProvider.GeminiThinkingLevel
				}
				if existingProvider.GeminiThinkingBudget != nil {
					geminiBudget = *existingProvider.GeminiThinkingBudget
				}
			}
		}

		if req.Provider.Temperature != nil {
			temperature = *req.Provider.Temperature
		}
		if req.Provider.TopP != nil {
			topP = *req.Provider.TopP
		}
		if req.Provider.ThinkingBudget != nil {
			thinkingBudget = *req.Provider.ThinkingBudget
		}
		if req.Provider.PromptCaching != nil {
			promptCaching = *req.Provider.PromptCaching
		}
		if req.Provider.PromptCacheTTL != nil {
			promptCacheTTL = normalizeAnthropicPromptCacheTTL(*req.Provider.PromptCacheTTL)
		}
		if req.Provider.ReasoningEffort != nil {
			reasoningEffort = *req.Provider.ReasoningEffort
		}
		if req.Provider.ContextMessages != nil {
			contextMessages = *req.Provider.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
			topP = 0
		}

		var geminiThinkingMode *string
		var geminiThinkingLevel *string
		var geminiThinkingBudget *int
		if providerType == config.ProviderTypeGemini {
			if req.Provider.GeminiThinkingMode != nil {
				geminiMode = strings.TrimSpace(*req.Provider.GeminiThinkingMode)
			}
			if req.Provider.GeminiThinkingLevel != nil {
				geminiLevel = strings.TrimSpace(*req.Provider.GeminiThinkingLevel)
			}
			if req.Provider.GeminiThinkingBudget != nil {
				geminiBudget = *req.Provider.GeminiThinkingBudget
			}
			if strings.TrimSpace(geminiMode) == "" {
				geminiMode = "none"
			}
			if strings.TrimSpace(geminiLevel) == "" {
				geminiLevel = "low"
			}
			geminiThinkingMode = &geminiMode
			geminiThinkingLevel = &geminiLevel
			geminiThinkingBudget = &geminiBudget
		}

		id := strings.TrimSpace(req.Provider.ID)
		if id == "" {
			id = "memory"
		}
		name := strings.TrimSpace(req.Provider.Name)
		if name == "" {
			name = "记忆提供商"
		}

		provider := config.Provider{
			ID:                   id,
			Name:                 name,
			Type:                 providerType,
			BaseURL:              baseURL,
			APIKey:               apiKey,
			Model:                model,
			Temperature:          temperature,
			TopP:                 topP,
			ThinkingBudget:       thinkingBudget,
			PromptCaching:        promptCaching,
			PromptCacheTTL:       promptCacheTTL,
			ReasoningEffort:      reasoningEffort,
			GeminiThinkingMode:   geminiThinkingMode,
			GeminiThinkingLevel:  geminiThinkingLevel,
			GeminiThinkingBudget: geminiThinkingBudget,
			ContextMessages:      contextMessages,
			Stream:               req.Provider.Stream,
			ImageCapable:         req.Provider.ImageCapable,
		}

		cfg.MemoryProvider = &provider
		cfg.MemoryProviderID = ""
		if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
			return
		}

		clone := provider
		if clone.Type == config.ProviderTypeAnthropic {
			clone.Temperature = 1
			clone.TopP = 0
		}
		if len(clone.APIKey) > 8 {
			clone.APIKey = clone.APIKey[:4] + "****" + clone.APIKey[len(clone.APIKey)-4:]
		} else if len(clone.APIKey) > 0 {
			clone.APIKey = "****"
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_provider": clone}})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func normalizeGeminiImageAspectRatio(aspectRatio string) string {
	aspectRatio = strings.TrimSpace(aspectRatio)
	if aspectRatio == "" {
		return "1:1"
	}
	switch aspectRatio {
	case "1:1", "3:4", "4:3", "9:16", "16:9":
		return aspectRatio
	default:
		return "1:1"
	}
}

func normalizeGeminiImageSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	switch strings.ToUpper(size) {
	case "1K":
		return "1K"
	case "2K":
		return "2K"
	default:
		return ""
	}
}

func clampGeminiImageNumberOfImages(numberOfImages int) int {
	if numberOfImages < 1 {
		return 1
	}
	if numberOfImages > 8 {
		return 8
	}
	return numberOfImages
}

func normalizeGeminiImageOutputMIMEType(outputMIMEType string) string {
	outputMIMEType = strings.ToLower(strings.TrimSpace(outputMIMEType))
	if outputMIMEType == "" {
		return "image/jpeg"
	}
	switch outputMIMEType {
	case "image/jpeg", "image/png":
		return outputMIMEType
	default:
		return "image/jpeg"
	}
}

func normalizeAnthropicPromptCacheTTL(ttl string) string {
	switch strings.ToLower(strings.TrimSpace(ttl)) {
	case "1h":
		return "1h"
	case "5m", "":
		return "5m"
	default:
		return "5m"
	}
}
