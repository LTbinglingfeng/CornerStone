package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

// Handler HTTP请求处理器
type Handler struct {
	configManager *config.Manager
	promptManager *storage.PromptManager
	chatManager   *storage.ChatManager
	userManager   *storage.UserManager
}

// NewHandler 创建处理器
func NewHandler(cm *config.Manager, pm *storage.PromptManager, chatMgr *storage.ChatManager, userMgr *storage.UserManager) *Handler {
	return &Handler{
		configManager: cm,
		promptManager: pm,
		chatManager:   chatMgr,
		userManager:   userMgr,
	}
}

// ChatMessageRequest 前端发送的聊天请求
type ChatMessageRequest struct {
	SessionID   string           `json:"session_id,omitempty"`
	PromptID    string           `json:"prompt_id,omitempty"` // 选择的人设ID
	Messages    []client.Message `json:"messages"`
	Stream      *bool            `json:"stream,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"` // 供应商类型 (openai/gemini)
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	Stream  bool   `json:"stream"` // 是否启用流式输出
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
	maxJSONBodyBytes   int64 = 8 << 20  // 8MB
	maxAvatarBodyBytes int64 = 11 << 20 // 10MB + overhead
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
	// 聊天接口 (保持 /api 前缀)
	mux.HandleFunc("/api/chat", h.corsMiddleware(h.handleChat))

	// 配置接口 (使用 /management 前缀)
	mux.HandleFunc("/management/config", h.corsMiddleware(h.handleConfig))

	// 供应商管理接口
	mux.HandleFunc("/management/providers", h.corsMiddleware(h.handleProviders))
	mux.HandleFunc("/management/providers/", h.corsMiddleware(h.handleProviderByID))
	mux.HandleFunc("/management/providers/active", h.corsMiddleware(h.handleActiveProvider))

	// 提示词接口
	mux.HandleFunc("/management/prompts", h.corsMiddleware(h.handlePrompts))
	mux.HandleFunc("/management/prompts/", h.corsMiddleware(h.handlePromptByID))

	// 提示词头像接口
	mux.HandleFunc("/management/prompts-avatar/", h.corsMiddleware(h.handlePromptAvatar))

	// 提示词相关聊天记录接口
	mux.HandleFunc("/management/prompts-sessions/", h.corsMiddleware(h.handlePromptSessions))

	// 聊天记录接口
	mux.HandleFunc("/management/sessions", h.corsMiddleware(h.handleSessions))
	mux.HandleFunc("/management/sessions/", h.corsMiddleware(h.handleSessionByID))

	// 用户信息接口
	mux.HandleFunc("/management/user", h.corsMiddleware(h.handleUser))
	mux.HandleFunc("/management/user/avatar", h.corsMiddleware(h.handleUserAvatar))

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

		provider := config.Provider{
			ID:      req.ID,
			Name:    req.Name,
			Type:    providerType,
			BaseURL: req.BaseURL,
			APIKey:  req.APIKey,
			Model:   req.Model,
			Stream:  req.Stream,
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

		provider := config.Provider{
			ID:      id,
			Name:    req.Name,
			Type:    providerType,
			BaseURL: req.BaseURL,
			APIKey:  req.APIKey,
			Model:   req.Model,
			Stream:  req.Stream,
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
	id := strings.TrimPrefix(r.URL.Path, "/management/sessions/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	switch r.Method {
	case "GET":
		session, ok := h.chatManager.GetSession(id)
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

		if err := h.chatManager.UpdateSessionTitle(id, req.Title); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session updated"})

	case "DELETE":
		if err := h.chatManager.DeleteSession(id); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
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
		h.chatManager.AddMessage(sessionID, lastMsg.Role, lastMsg.Content)
	}

	// 构建消息，顺序: 系统提示词 -> 用户信息 -> 人设
	messages := make([]client.Message, 0, len(req.Messages)+1)

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

	if fullSystemPrompt != "" {
		messages = append(messages, client.Message{
			Role:    "system",
			Content: fullSystemPrompt,
		})
	}
	messages = append(messages, req.Messages...)

	// 根据供应商类型创建对应的客户端
	var aiClient client.AIClient
	switch provider.Type {
	case config.ProviderTypeGemini:
		aiClient = client.NewGeminiClient(provider.BaseURL, provider.APIKey)
	default:
		// 默认使用 OpenAI 兼容客户端
		aiClient = client.NewClient(provider.BaseURL, provider.APIKey)
	}

	// 默认使用供应商配置；如果请求中明确指定 stream，则以请求为准
	useStream := provider.Stream
	if req.Stream != nil {
		useStream = *req.Stream
	}

	chatReq := client.ChatRequest{
		Model:       provider.Model,
		Messages:    messages,
		Stream:      useStream,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       getRedPacketTools(),
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
		content := resp.Choices[0].Message.Content
		reasoningContent := resp.Choices[0].Message.ReasoningContent
		h.chatManager.AddMessageWithReasoning(sessionID, "assistant", content, reasoningContent)
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

	err := aiClient.ChatStream(r.Context(), req, func(chunk client.StreamChunk) error {
		// 收集完整内容
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" {
				fullContent.WriteString(chunk.Choices[0].Delta.Content)
			}
			// 收集思考内容
			if chunk.Choices[0].Delta.ReasoningContent != "" {
				fullReasoningContent.WriteString(chunk.Choices[0].Delta.ReasoningContent)
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

	if err != nil {
		log.Printf("Stream error: %v", err)
		errorPayload, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errorPayload)
		flusher.Flush()
	}

	// 保存AI回复到历史记录（包含思考内容）
	if saveHistory && (fullContent.Len() > 0 || fullReasoningContent.Len() > 0) {
		h.chatManager.AddMessageWithReasoning(sessionID, "assistant", fullContent.String(), fullReasoningContent.String())
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (h *Handler) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
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

// getRedPacketTools 返回红包工具定义
func getRedPacketTools() []client.Tool {
	return []client.Tool{
		{
			Type: "function",
			Function: client.ToolFunction{
				Name:        "send_red_packet",
				Description: "向用户发送一个红包。当你想要给用户发红包、送礼物、表达祝福时使用此工具。",
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
	}
}
