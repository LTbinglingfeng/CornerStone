package api

import (
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
}

// NewHandler 创建处理器
func NewHandler(cm *config.Manager, pm *storage.PromptManager, chatMgr *storage.ChatManager, userMgr *storage.UserManager, authMgr *storage.AuthManager, cachePhotoDir string) *Handler {
	return &Handler{
		configManager: cm,
		promptManager: pm,
		chatManager:   chatMgr,
		userManager:   userMgr,
		authManager:   authMgr,
		tokenStore:    newAuthTokenStore(),
		cachePhotoDir: cachePhotoDir,
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
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Type                 string   `json:"type"` // 供应商类型 (openai/openai_response/gemini/anthropic)
	BaseURL              string   `json:"base_url"`
	APIKey               string   `json:"api_key"`
	Model                string   `json:"model"`
	Temperature          *float64 `json:"temperature,omitempty"`
	TopP                 *float64 `json:"top_p,omitempty"`
	ThinkingBudget       *int     `json:"thinking_budget,omitempty"`
	ReasoningEffort      *string  `json:"reasoning_effort,omitempty"`
	GeminiThinkingMode   *string  `json:"gemini_thinking_mode,omitempty"`
	GeminiThinkingLevel  *string  `json:"gemini_thinking_level,omitempty"`
	GeminiThinkingBudget *int     `json:"gemini_thinking_budget,omitempty"`
	ContextMessages      *int     `json:"context_messages,omitempty"`
	Stream               bool     `json:"stream"`        // 是否启用流式输出
	ImageCapable         bool     `json:"image_capable"` // 是否支持识图
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

	// 配置接口 (使用 /management 前缀)
	mux.HandleFunc("/management/config", h.corsMiddleware(h.authMiddleware(h.handleConfig)))

	// 供应商管理接口
	mux.HandleFunc("/management/providers", h.corsMiddleware(h.authMiddleware(h.handleProviders)))
	mux.HandleFunc("/management/providers/", h.corsMiddleware(h.authMiddleware(h.handleProviderByID)))
	mux.HandleFunc("/management/providers/active", h.corsMiddleware(h.authMiddleware(h.handleActiveProvider)))

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
		result := map[string]interface{}{
			"providers":          providers,
			"active_provider_id": activeID,
			"system_prompt":      h.configManager.GetSystemPrompt(),
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

		provider := config.Provider{
			ID:                   req.ID,
			Name:                 req.Name,
			Type:                 providerType,
			BaseURL:              req.BaseURL,
			APIKey:               req.APIKey,
			Model:                req.Model,
			Temperature:          temperature,
			TopP:                 topP,
			ThinkingBudget:       thinkingBudget,
			ReasoningEffort:      reasoningEffort,
			GeminiThinkingMode:   geminiThinkingMode,
			GeminiThinkingLevel:  geminiThinkingLevel,
			GeminiThinkingBudget: geminiThinkingBudget,
			ContextMessages:      contextMessages,
			Stream:               req.Stream,
			ImageCapable:         req.ImageCapable,
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

		provider := config.Provider{
			ID:                   id,
			Name:                 req.Name,
			Type:                 providerType,
			BaseURL:              req.BaseURL,
			APIKey:               apiKey,
			Model:                req.Model,
			Temperature:          temperature,
			TopP:                 topP,
			ThinkingBudget:       thinkingBudget,
			ReasoningEffort:      reasoningEffort,
			GeminiThinkingMode:   geminiThinkingMode,
			GeminiThinkingLevel:  geminiThinkingLevel,
			GeminiThinkingBudget: geminiThinkingBudget,
			ContextMessages:      contextMessages,
			Stream:               req.Stream,
			ImageCapable:         req.ImageCapable,
		}

		if err := h.configManager.UpdateProvider(provider); err != nil {
			if err == config.ErrProviderNotFound {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: err.Error()})
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

	// 获取人设（prompt）
	var persona string
	if req.PromptID != "" {
		if prompt, ok := h.promptManager.Get(req.PromptID); ok {
			persona = prompt.Content
		}
	}

	// 生成或使用会话ID
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateID()
	}

	// 保存用户消息到历史记录
	if req.SaveHistory && len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if errSaveHistory := h.chatManager.AddMessageWithDetails(sessionID, lastMsg.Role, lastMsg.Content, lastMsg.ReasoningContent, lastMsg.ImagePaths, lastMsg.ToolCalls); errSaveHistory != nil {
			if errors.Is(errSaveHistory, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errSaveHistory.Error()})
			return
		}
	}

	historyMessages := req.Messages
	if req.SaveHistory {
		if session, ok := h.chatManager.GetSession(sessionID); ok && len(session.Messages) > 0 {
			historyMessages = convertChatMessages(session.Messages)
		}
	}
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
		h.handleStreamChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory)
	} else {
		h.handleNormalChat(w, r, aiClient, chatReq, sessionID, req.SaveHistory)
	}
}

// handleNormalChat 处理非流式聊天
func (h *Handler) handleNormalChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool) {
	resp, err := aiClient.Chat(r.Context(), req)
	if err != nil {
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

	// 返回响应，包含session_id
	result := map[string]interface{}{
		"session_id": sessionID,
		"response":   resp,
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})
}

// handleStreamChat 处理流式聊天 (SSE)
func (h *Handler) handleStreamChat(w http.ResponseWriter, r *http.Request, aiClient client.AIClient, req client.ChatRequest, sessionID string, saveHistory bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Streaming not supported"})
		return
	}

	// 发送session_id
	sessionPayload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	fmt.Fprintf(w, "data: %s\n\n", sessionPayload)
	flusher.Flush()

	var fullContent strings.Builder
	var fullReasoningContent strings.Builder
	toolCallsMap := make(map[int]*client.ToolCall)

	errChatStream := aiClient.ChatStream(r.Context(), req, func(chunk client.StreamChunk) error {
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

		data, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return nil
	})

	if errChatStream != nil {
		logging.Errorf("Stream error: %v", errChatStream)
		errorPayload, _ := json.Marshal(map[string]string{"error": errChatStream.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errorPayload)
		flusher.Flush()
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
			errorPayload, _ := json.Marshal(map[string]string{"error": errorMessage})
			fmt.Fprintf(w, "data: %s\n\n", errorPayload)
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
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
