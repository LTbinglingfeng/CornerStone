package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Handler HTTP请求处理器
type Handler struct {
	configManager *config.Manager
	promptManager *storage.PromptManager
	chatManager   *storage.ChatManager
	userManager   *storage.UserManager
	authManager   *storage.AuthManager
	tokenStore    *authTokenStore
	cachePhotoDir string

	memoryManager   *storage.MemoryManager
	memoryExtractor *storage.MemoryExtractor
	memorySessions  map[string]*storage.MemorySession
	sessionsMu      sync.RWMutex

	cleanupOnce sync.Once
	cleanupDone chan struct{}
}

const (
	memorySessionMaxIdle     = 30 * time.Minute // 会话最大空闲时间
	memorySessionCleanupTick = 5 * time.Minute  // 清理检查间隔
)

// NewHandler 创建处理器
func NewHandler(cm *config.Manager, pm *storage.PromptManager, chatMgr *storage.ChatManager, userMgr *storage.UserManager, authMgr *storage.AuthManager, cachePhotoDir string, memoryManager *storage.MemoryManager, memoryExtractor *storage.MemoryExtractor) *Handler {
	return &Handler{
		configManager:   cm,
		promptManager:   pm,
		chatManager:     chatMgr,
		userManager:     userMgr,
		authManager:     authMgr,
		tokenStore:      newAuthTokenStore(),
		cachePhotoDir:   cachePhotoDir,
		memoryManager:   memoryManager,
		memoryExtractor: memoryExtractor,
		memorySessions:  make(map[string]*storage.MemorySession),
		cleanupDone:     make(chan struct{}),
	}
}

// ChatMessageRequest 前端发送的聊天请求
type ChatMessageRequest struct {
	SessionID   string           `json:"session_id,omitempty"`
	PromptID    string           `json:"prompt_id,omitempty"` // 选择的人设ID
	Messages    []client.Message `json:"messages"`
	Stream      *bool            `json:"stream,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	SaveHistory bool             `json:"save_history,omitempty"`
}

// ConfigUpdateRequest 配置更新请求
type ConfigUpdateRequest struct {
	BaseURL      *string `json:"base_url,omitempty"`
	APIKey       *string `json:"api_key,omitempty"`
	Model        *string `json:"model,omitempty"`
	SystemPrompt *string `json:"system_prompt,omitempty"`
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

// PromptRequest 提示词请求
type PromptRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Content     string `json:"content"`
	Description string `json:"description,omitempty"`
	FileName    string `json:"file_name,omitempty"`
}

// SessionRequest 会话请求
type SessionRequest struct {
	Title      string `json:"title,omitempty"`
	PromptID   string `json:"prompt_id,omitempty"`
	PromptName string `json:"prompt_name,omitempty"`
}

type SessionMessageUpdateRequest struct {
	Index   int    `json:"index"`
	Content string `json:"content"`
}

type SessionMessageDeleteRequest struct {
	Index int `json:"index"`
}

type SessionRedPacketOpenRequest struct {
	PacketKey    string `json:"packet_key"`
	ReceiverName string `json:"receiver_name,omitempty"`
	SenderName   string `json:"sender_name,omitempty"`
}

// UserInfoRequest 用户信息请求
type UserInfoRequest struct {
	Username    string `json:"username,omitempty"`
	Description string `json:"description,omitempty"`
}

type MemoryUpsertRequest struct {
	Subject  string   `json:"subject,omitempty"`
	Category string   `json:"category,omitempty"`
	Content  string   `json:"content,omitempty"`
	Strength *float64 `json:"strength,omitempty"`
}

type SetMemoryProviderRequest struct {
	ProviderID string `json:"provider_id"`
}

type SetMemoryEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

type MemoryProviderConfigRequest struct {
	UseCustom bool             `json:"use_custom"`
	Provider  *ProviderRequest `json:"provider,omitempty"`
}

// Response 统一响应格式
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

const (
	maxJSONBodyBytes      int64 = 8 << 20  // 8MB
	maxAvatarBodyBytes    int64 = 11 << 20 // 10MB + overhead
	maxChatImageBodyBytes int64 = 11 << 20 // 10MB + overhead

	cachePhotoDirName = "cache_photo"
)

func (h *Handler) decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
			return false
		}
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid JSON"})
		return false
	}
	return true
}

// RegisterRoutes 注册所有路由
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// 鉴权接口
	mux.HandleFunc("/management/auth/status", h.corsMiddleware(h.handleAuthStatus))
	mux.HandleFunc("/management/auth/setup", h.corsMiddleware(h.handleAuthSetup))
	mux.HandleFunc("/management/auth/login", h.corsMiddleware(h.handleAuthLogin))

	// 聊天接口 (保持 /api 前缀)
	mux.HandleFunc("/api/chat", h.corsMiddleware(h.authMiddleware(h.handleChat)))

	// 记忆接口
	mux.HandleFunc("/api/memory/", h.corsMiddleware(h.authMiddleware(h.handleMemory)))
	mux.HandleFunc("/api/settings/memory-provider", h.corsMiddleware(h.authMiddleware(h.handleSetMemoryProvider)))
	mux.HandleFunc("/api/settings/memory-enabled", h.corsMiddleware(h.authMiddleware(h.handleSetMemoryEnabled)))

	// 配置接口 (使用 /management 前缀)
	mux.HandleFunc("/management/config", h.corsMiddleware(h.authMiddleware(h.handleConfig)))

	// 供应商管理接口
	mux.HandleFunc("/management/providers", h.corsMiddleware(h.authMiddleware(h.handleProviders)))
	mux.HandleFunc("/management/providers/", h.corsMiddleware(h.authMiddleware(h.handleProviderByID)))
	mux.HandleFunc("/management/providers/active", h.corsMiddleware(h.authMiddleware(h.handleActiveProvider)))
	mux.HandleFunc("/management/memory-provider", h.corsMiddleware(h.authMiddleware(h.handleMemoryProvider)))

	// 提示词接口
	mux.HandleFunc("/management/prompts", h.corsMiddleware(h.authMiddleware(h.handlePrompts)))
	mux.HandleFunc("/management/prompts/", h.corsMiddleware(h.authMiddleware(h.handlePromptByID)))

	// 提示词头像接口
	mux.HandleFunc("/management/prompts-avatar/", h.corsMiddleware(h.authMiddleware(h.handlePromptAvatar)))

	// 提示词相关聊天记录接口
	mux.HandleFunc("/management/prompts-sessions/", h.corsMiddleware(h.authMiddleware(h.handlePromptSessions)))

	// 聊天记录接口
	mux.HandleFunc("/management/sessions", h.corsMiddleware(h.authMiddleware(h.handleSessions)))
	mux.HandleFunc("/management/sessions/", h.corsMiddleware(h.authMiddleware(h.handleSessionByID)))

	// 用户信息接口
	mux.HandleFunc("/management/user", h.corsMiddleware(h.authMiddleware(h.handleUser)))
	mux.HandleFunc("/management/user/avatar", h.corsMiddleware(h.authMiddleware(h.handleUserAvatar)))

	// 聊天图片缓存接口
	mux.HandleFunc("/management/cache-photo", h.corsMiddleware(h.authMiddleware(h.handleCachePhoto)))
	mux.HandleFunc("/management/cache-photo/", h.corsMiddleware(h.authMiddleware(h.handleCachePhotoByName)))

	// 健康检查
	mux.HandleFunc("/management/health", h.corsMiddleware(h.handleHealth))
}

// corsMiddleware 处理跨域请求
func (h *Handler) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// handleHealth 健康检查
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "OK"})
}

// handleConfig 处理配置请求（兼容旧API）
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
			BaseURL      string `json:"base_url"`
			APIKey       string `json:"api_key"`
			Model        string `json:"model"`
			SystemPrompt string `json:"system_prompt"`
		}{
			BaseURL:      provider.BaseURL,
			APIKey:       provider.APIKey,
			Model:        provider.Model,
			SystemPrompt: h.configManager.GetSystemPrompt(),
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
			}
			if len(clone.APIKey) > 8 {
				clone.APIKey = clone.APIKey[:4] + "****" + clone.APIKey[len(clone.APIKey)-4:]
			} else if len(clone.APIKey) > 0 {
				clone.APIKey = "****"
			}
			memoryProvider = &clone
		}
		result := map[string]interface{}{
			"providers":          providers,
			"active_provider_id": activeID,
			"system_prompt":      h.configManager.GetSystemPrompt(),
			"memory_provider_id": cfg.MemoryProviderID,
			"memory_provider":    memoryProvider,
			"memory_enabled":     cfg.MemoryEnabled,
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})

	case "POST":
		var req ProviderRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

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
		if req.ReasoningEffort != nil {
			reasoningEffort = *req.ReasoningEffort
		}
		if req.ContextMessages != nil {
			contextMessages = *req.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
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
			BaseURL:                   req.BaseURL,
			APIKey:                    req.APIKey,
			Model:                     req.Model,
			Temperature:               temperature,
			TopP:                      topP,
			ThinkingBudget:            thinkingBudget,
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

		temperature := defaultProvider.Temperature
		topP := defaultProvider.TopP
		thinkingBudget := defaultProvider.ThinkingBudget
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
		if req.ReasoningEffort != nil {
			reasoningEffort = *req.ReasoningEffort
		}
		if req.ContextMessages != nil {
			contextMessages = *req.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
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
			BaseURL:                   req.BaseURL,
			APIKey:                    apiKey,
			Model:                     req.Model,
			Temperature:               temperature,
			TopP:                      topP,
			ThinkingBudget:            thinkingBudget,
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

		defaultProvider := config.DefaultProvider()
		temperature := defaultProvider.Temperature
		topP := defaultProvider.TopP
		thinkingBudget := defaultProvider.ThinkingBudget
		reasoningEffort := defaultProvider.ReasoningEffort
		contextMessages := defaultProvider.ContextMessages

		geminiMode := "none"
		geminiLevel := "low"
		geminiBudget := 128

		if existingProvider != nil {
			temperature = existingProvider.Temperature
			topP = existingProvider.TopP
			thinkingBudget = existingProvider.ThinkingBudget
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
		if req.Provider.ReasoningEffort != nil {
			reasoningEffort = *req.Provider.ReasoningEffort
		}
		if req.Provider.ContextMessages != nil {
			contextMessages = *req.Provider.ContextMessages
		}
		if providerType == config.ProviderTypeAnthropic {
			temperature = 1
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
			BaseURL:              strings.TrimSpace(req.Provider.BaseURL),
			APIKey:               apiKey,
			Model:                strings.TrimSpace(req.Provider.Model),
			Temperature:          temperature,
			TopP:                 topP,
			ThinkingBudget:       thinkingBudget,
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

// handlePrompts 处理提示词列表请求
func (h *Handler) handlePrompts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		prompts := h.promptManager.List()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompts})

	case "POST":
		var req PromptRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" || req.Content == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Name and content are required"})
			return
		}

		id := req.ID
		if id == "" {
			id = generatePromptID()
		}

		prompt, err := h.promptManager.Create(id, req.Name, req.Content, req.Description, req.FileName)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: prompt})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handlePromptByID 处理单个提示词请求
func (h *Handler) handlePromptByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/management/prompts/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		prompt, ok := h.promptManager.Get(id)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompt})

	case "PUT":
		var req PromptRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		prompt, err := h.promptManager.Update(id, req.Name, req.Content, req.Description, req.FileName)
		if err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompt})

	case "DELETE":
		if err := h.promptManager.Delete(id); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Prompt deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handlePromptSessions 处理获取指定 Prompt 的所有聊天记录请求
func (h *Handler) handlePromptSessions(w http.ResponseWriter, r *http.Request) {
	promptID := strings.TrimPrefix(r.URL.Path, "/management/prompts-sessions/")
	if promptID == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		// 先检查 prompt 是否存在
		if _, ok := h.promptManager.Get(promptID); !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}

		sessions := h.chatManager.GetSessionsByPromptID(promptID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: sessions})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleSessions 处理会话列表请求
func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		sessions := h.chatManager.ListSessions()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: sessions})

	case "POST":
		var req SessionRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		// 如果提供了 promptID 但没有提供 promptName，尝试从 promptManager 获取
		promptID := req.PromptID
		promptName := req.PromptName
		if promptID != "" && promptName == "" {
			if prompt, ok := h.promptManager.Get(promptID); ok {
				promptName = prompt.Name
			}
		}

		title := req.Title
		if title == "" {
			if promptName != "" {
				title = promptName
			} else {
				title = "New Chat"
			}
		}

		sessionID := generateID()
		session, err := h.chatManager.CreateSession(sessionID, title, promptID, promptName)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: session})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleSessionByID 处理单个会话请求
func (h *Handler) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/management/sessions/")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	parts := strings.Split(raw, "/")
	sessionID := parts[0]
	if sessionID == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	if len(parts) >= 2 && parts[1] == "messages" {
		h.handleSessionMessages(w, r, sessionID, parts[2:])
		return
	}

	switch r.Method {
	case "GET":
		session, ok := h.chatManager.GetSession(sessionID)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: session})

	case "PUT":
		var req SessionRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if err := h.chatManager.UpdateSessionTitle(sessionID, req.Title); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session updated"})

	case "DELETE":
		if err := h.chatManager.DeleteSession(sessionID); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string, parts []string) {
	if len(parts) == 0 {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Message action required"})
		return
	}

	action := parts[0]
	switch action {
	case "update":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.UpdateMessageContentByIndex(sessionID, req.Index, req.Content); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "recall":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageDeleteRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.RecallMessageByIndex(sessionID, req.Index); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrPermission) {
				h.jsonResponse(w, http.StatusForbidden, Response{Success: false, Error: "Only user messages can be recalled"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "delete":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageDeleteRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.DeleteMessageByIndex(sessionID, req.Index); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "red-packet-open":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}

		var req SessionRedPacketOpenRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if err := h.chatManager.AddRedPacketReceivedBanner(sessionID, req.PacketKey, req.ReceiverName, req.SenderName); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid red packet key"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	default:
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Unknown message action"})
		return
	}
}

// handleChat 处理聊天请求
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
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "No active provider configured"})
		return
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Active provider is not chat-capable"})
		return
	}
	if provider.APIKey == "" {
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
	if effectivePromptID != "" {
		if prompt, ok := h.promptManager.Get(effectivePromptID); ok {
			persona = prompt.Content
		}
	}

	memSession := h.getOrCreateMemorySession(effectivePromptID, sessionID)
	if memSession != nil {
		persona = storage.BuildPromptWithMemory(persona, memSession.GetActiveMemories())
	}

	// 保存用户消息到历史记录
	if req.SaveHistory && len(req.Messages) > 0 {
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
		h.handleStreamChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession)
	} else {
		h.handleNormalChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory, memSession)
	}
}

func (h *Handler) getOrCreateMemorySession(promptID, sessionID string) *storage.MemorySession {
	if h.memoryManager == nil || h.configManager == nil {
		return nil
	}
	if sessionID == "" {
		return nil
	}

	cfg := h.configManager.Get()
	if !cfg.MemoryEnabled {
		return nil
	}

	// 启动清理 goroutine（只执行一次）
	h.cleanupOnce.Do(func() {
		go h.cleanupMemorySessions()
	})

	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	if h.memorySessions == nil {
		h.memorySessions = make(map[string]*storage.MemorySession)
	}

	if session, ok := h.memorySessions[sessionID]; ok {
		return session
	}
	if promptID == "" {
		return nil
	}

	session := storage.NewMemorySession(promptID, sessionID, h.memoryManager, h.memoryExtractor)
	h.memorySessions[sessionID] = session
	return session
}

// cleanupMemorySessions 定期清理空闲的记忆会话
func (h *Handler) cleanupMemorySessions() {
	ticker := time.NewTicker(memorySessionCleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-h.cleanupDone:
			return
		case <-ticker.C:
			h.doCleanupMemorySessions()
		}
	}
}

func (h *Handler) doCleanupMemorySessions() {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	now := time.Now()
	for sessionID, session := range h.memorySessions {
		if now.Sub(session.LastAccess()) > memorySessionMaxIdle {
			delete(h.memorySessions, sessionID)
			logging.Infof("清理空闲记忆会话: %s", sessionID)
		}
	}
}

func (h *Handler) handleMemory(w http.ResponseWriter, r *http.Request) {
	if h.memoryManager == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Memory manager not configured"})
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/api/memory/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 1:
		promptID := parts[0]
		switch r.Method {
		case http.MethodGet:
			h.handleGetMemories(w, r, promptID)
		case http.MethodPost:
			h.handleAddMemory(w, r, promptID)
		default:
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		}

	case 2:
		promptID := parts[0]
		memoryID := parts[1]
		switch r.Method {
		case http.MethodPut:
			h.handleUpdateMemory(w, r, promptID, memoryID)
		case http.MethodDelete:
			h.handleDeleteMemory(w, r, promptID, memoryID)
		default:
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		}

	default:
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Not found"})
	}
}

func (h *Handler) handleGetMemories(w http.ResponseWriter, r *http.Request, promptID string) {
	responses := h.memoryManager.GetAllResponses(promptID)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: responses})
}

func (h *Handler) handleAddMemory(w http.ResponseWriter, r *http.Request, promptID string) {
	var req MemoryUpsertRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Category) == "" || strings.TrimSpace(req.Content) == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "subject/category/content required"})
		return
	}
	if req.Subject != storage.SubjectUser && req.Subject != storage.SubjectSelf {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "invalid subject"})
		return
	}

	memory := storage.Memory{
		Subject:   strings.TrimSpace(req.Subject),
		Category:  strings.TrimSpace(req.Category),
		Content:   strings.TrimSpace(req.Content),
		SeenCount: 1,
	}

	if req.Strength == nil {
		memory.Strength = storage.DefaultStrengthForCategory(memory.Category)
	} else {
		memory.Strength = *req.Strength
	}

	if errAdd := h.memoryManager.Add(promptID, memory); errAdd != nil {
		if errors.Is(errAdd, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errAdd.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleUpdateMemory(w http.ResponseWriter, r *http.Request, promptID, memoryID string) {
	var req MemoryUpsertRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	patch := storage.MemoryPatch{
		ID: memoryID,
	}
	subject := strings.TrimSpace(req.Subject)
	if subject != "" {
		if subject != storage.SubjectUser && subject != storage.SubjectSelf {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "invalid subject"})
			return
		}
		patch.Subject = &subject
	}
	category := strings.TrimSpace(req.Category)
	if category != "" {
		patch.Category = &category
	}
	content := strings.TrimSpace(req.Content)
	if content != "" {
		patch.Content = &content
	}
	if req.Strength != nil {
		patch.Strength = req.Strength
	}

	if errUpdate := h.memoryManager.Patch(promptID, patch); errUpdate != nil {
		if errors.Is(errUpdate, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID or memory ID"})
			return
		}
		if errors.Is(errUpdate, os.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Memory not found"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleDeleteMemory(w http.ResponseWriter, r *http.Request, promptID, memoryID string) {
	if errDelete := h.memoryManager.Delete(promptID, memoryID); errDelete != nil {
		if errors.Is(errDelete, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID or memory ID"})
			return
		}
		if errors.Is(errDelete, os.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Memory not found"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errDelete.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleSetMemoryProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req SetMemoryProviderRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	providerID := strings.TrimSpace(req.ProviderID)
	if providerID != "" {
		provider := h.configManager.GetProvider(providerID)
		if provider == nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Provider not found"})
			return
		}
		if provider.Type == config.ProviderTypeGeminiImage {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider is not chat-capable"})
			return
		}
	}

	cfg := h.configManager.Get()
	cfg.MemoryProviderID = providerID
	cfg.MemoryProvider = nil
	if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_provider_id": providerID}})
}

func (h *Handler) handleSetMemoryEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req SetMemoryEnabledRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	cfg := h.configManager.Get()
	cfg.MemoryEnabled = req.Enabled
	if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_enabled": req.Enabled}})
}

// handleNormalChat 处理非流式聊天
func (h *Handler) handleNormalChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession) {
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
	if saveHistory && len(resp.Choices) > 0 {
		message := resp.Choices[0].Message
		if errSaveHistory := h.chatManager.AddMessageWithDetails(sessionID, "assistant", message.Content, message.ReasoningContent, nil, message.ToolCalls); errSaveHistory != nil {
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

	// 返回响应，包含session_id
	result := map[string]interface{}{
		"session_id": sessionID,
		"response":   resp,
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})
}

// handleStreamChat 处理流式聊天 (SSE)
func (h *Handler) handleStreamChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool, memSession *storage.MemorySession) {
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
	if saveHistory && (fullContent.Len() > 0 || fullReasoningContent.Len() > 0 || len(toolCalls) > 0) {
		if errSaveHistory := h.chatManager.AddMessageWithDetails(sessionID, "assistant", fullContent.String(), fullReasoningContent.String(), nil, toolCalls); errSaveHistory != nil {
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

func (h *Handler) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) resolveCachePhotoPath(relPath string) (string, error) {
	cleaned := strings.ReplaceAll(relPath, "\\", "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid image path")
	}

	cleaned = strings.TrimPrefix(cleaned, "./")
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid image path")
	}

	fileName := cleaned
	if strings.HasPrefix(cleaned, cachePhotoDirName+"/") {
		fileName = strings.TrimPrefix(cleaned, cachePhotoDirName+"/")
	}
	if errValidateFileName := storage.ValidateFileName(fileName); errValidateFileName != nil {
		return "", errValidateFileName
	}

	return filepath.Join(h.cachePhotoDir, fileName), nil
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

// handleCachePhoto 处理聊天图片上传
func (h *Handler) handleCachePhoto(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatImageBodyBytes)
	if errParseForm := r.ParseMultipartForm(10 << 20); errParseForm != nil {
		var maxErr *http.MaxBytesError
		if errors.As(errParseForm, &maxErr) {
			h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
			return
		}
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
		return
	}

	file, header, errFormFile := r.FormFile("image")
	if errFormFile != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get image file"})
		return
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			logging.Warnf("close upload file error: %v", errClose)
		}
	}()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".png"
	}

	filename := generateID() + ext
	if errValidateFileName := storage.ValidateFileName(filename); errValidateFileName != nil {
		filename = generateID() + ".png"
	}
	if errValidateFileName := storage.ValidateFileName(filename); errValidateFileName != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid image filename"})
		return
	}

	savedPath := filepath.Join(h.cachePhotoDir, filename)
	output, errCreate := os.Create(savedPath)
	if errCreate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errCreate.Error()})
		return
	}
	defer func() {
		if errClose := output.Close(); errClose != nil {
			logging.Warnf("close cached image error: %v", errClose)
		}
	}()

	if _, errCopy := io.Copy(output, file); errCopy != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errCopy.Error()})
		return
	}

	relPath := path.Join(cachePhotoDirName, filename)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: relPath})
}

// handleCachePhotoByName 读取聊天图片
func (h *Handler) handleCachePhotoByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/management/cache-photo/")
	if name == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Image name required"})
		return
	}

	relPath := path.Join(cachePhotoDirName, name)
	imagePath, errResolve := h.resolveCachePhotoPath(relPath)
	if errResolve != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Image not found"})
		return
	}
	if _, errStat := os.Stat(imagePath); errStat != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Image not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(imagePath))
	contentType := "application/octet-stream"
	switch ext {
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".png":
		contentType = "image/png"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, imagePath)
}

// handlePromptAvatar 处理提示词头像请求
func (h *Handler) handlePromptAvatar(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/management/prompts-avatar/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		// 获取头像
		avatarPath, err := h.promptManager.GetAvatarPath(id)
		if err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Avatar not found"})
			return
		}

		// 根据文件扩展名设置Content-Type
		ext := strings.ToLower(filepath.Ext(avatarPath))
		contentType := "application/octet-stream"
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".svg":
			contentType = "image/svg+xml"
		}

		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, avatarPath)

	case "POST":
		// 上传头像
		r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBodyBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
				return
			}
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
			return
		}

		file, header, err := r.FormFile("avatar")
		if err != nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get avatar file"})
			return
		}
		defer file.Close()

		// 获取文件扩展名
		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".png"
		}

		// 使用 avatar + 扩展名作为文件名
		filename := "avatar" + ext

		savedFilename, err := h.promptManager.SaveAvatar(id, filename, file)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: savedFilename})

	case "DELETE":
		// 删除头像
		prompt, ok := h.promptManager.Get(id)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}

		if prompt.Avatar == "" {
			h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "No avatar to delete"})
			return
		}

		// 设置头像为空
		if err := h.promptManager.SetAvatar(id, ""); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Avatar deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// generateID 生成唯一ID
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generatePromptID 生成Prompt专用ID (3位数字+3位英文字母)
func generatePromptID() string {
	// 生成3位数字
	numBytes := make([]byte, 2)
	rand.Read(numBytes)
	num := int(numBytes[0])<<8 | int(numBytes[1])
	num = num % 1000 // 0-999

	// 生成3位字母
	letters := "abcdefghijklmnopqrstuvwxyz"
	letterBytes := make([]byte, 3)
	rand.Read(letterBytes)

	result := fmt.Sprintf("%03d", num)
	for i := 0; i < 3; i++ {
		result += string(letters[int(letterBytes[i])%len(letters)])
	}

	return result
}

// handleUser 处理用户信息请求
func (h *Handler) handleUser(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		userInfo := h.userManager.Get()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: userInfo})

	case "PUT", "POST":
		var req UserInfoRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		userInfo, err := h.userManager.Update(req.Username, req.Description)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		if req.Username != "" && h.authManager != nil {
			if err := h.authManager.UpdateUsername(req.Username); err != nil && !errors.Is(err, storage.ErrAuthNotSetup) {
				logging.Errorf("sync auth username failed: %v", err)
			}
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: userInfo})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleUserAvatar 处理用户头像请求
func (h *Handler) handleUserAvatar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		avatarPath, err := h.userManager.GetAvatarPath()
		if err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Avatar not found"})
			return
		}

		// 根据文件扩展名设置Content-Type
		ext := strings.ToLower(filepath.Ext(avatarPath))
		contentType := "application/octet-stream"
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".svg":
			contentType = "image/svg+xml"
		}

		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, avatarPath)

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBodyBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
				return
			}
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
			return
		}

		file, header, err := r.FormFile("avatar")
		if err != nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get avatar file"})
			return
		}
		defer file.Close()

		// 获取文件扩展名
		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".png"
		}

		// 使用 avatar + 扩展名作为文件名
		filename := "avatar" + ext

		savedFilename, err := h.userManager.SaveAvatar(filename, file)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: savedFilename})

	case "DELETE":
		if err := h.userManager.DeleteAvatar(); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Avatar deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// getChatTools 返回聊天工具定义
func getChatTools() []client.Tool {
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
	}
}
