package api

import (
	"bytes"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
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
	// 鉴权接口
	h.registerPublicRoute(mux, "/management/auth/status", h.handleAuthStatus)
	h.registerPublicRoute(mux, "/management/auth/setup", h.handleAuthSetup)
	h.registerPublicRoute(mux, "/management/auth/login", h.handleAuthLogin)

	// 聊天接口 (保持 /api 前缀)
	h.registerProtectedRoute(mux, "/api/chat", h.handleChat)

	// 记忆接口
	h.registerProtectedRoute(mux, "/api/memory/", h.handleMemory)
	h.registerProtectedRoute(mux, "/api/settings/memory-provider", h.handleSetMemoryProvider)
	h.registerProtectedRoute(mux, "/api/settings/memory-enabled", h.handleSetMemoryEnabled)

	// 配置接口 (使用 /management 前缀)
	h.registerProtectedRoute(mux, "/management/config", h.handleConfig)

	// 供应商管理接口
	h.registerProtectedRoute(mux, "/management/providers", h.handleProviders)
	h.registerProtectedRoute(mux, "/management/providers/", h.handleProviderByID)
	h.registerProtectedRoute(mux, "/management/providers/active", h.handleActiveProvider)
	h.registerProtectedRoute(mux, "/management/memory-provider", h.handleMemoryProvider)

	// 提示词接口
	h.registerProtectedRoute(mux, "/management/prompts", h.handlePrompts)
	h.registerProtectedRoute(mux, "/management/prompts/", h.handlePromptByID)

	// 提示词头像接口
	h.registerProtectedRoute(mux, "/management/prompts-avatar/", h.handlePromptAvatar)

	// 提示词相关聊天记录接口
	h.registerProtectedRoute(mux, "/management/prompts-sessions/", h.handlePromptSessions)

	// 聊天记录接口
	h.registerProtectedRoute(mux, "/management/sessions", h.handleSessions)
	h.registerProtectedRoute(mux, "/management/sessions/", h.handleSessionByID)

	// 用户信息接口
	h.registerProtectedRoute(mux, "/management/user", h.handleUser)
	h.registerProtectedRoute(mux, "/management/user/avatar", h.handleUserAvatar)

	// 聊天图片缓存接口
	h.registerProtectedRoute(mux, "/management/cache-photo", h.handleCachePhoto)
	h.registerProtectedRoute(mux, "/management/cache-photo/", h.handleCachePhotoByName)

	// 健康检查
	h.registerPublicRoute(mux, "/management/health", h.handleHealth)
}

func (h *Handler) registerPublicRoute(mux *http.ServeMux, path string, handler http.HandlerFunc) {
	mux.HandleFunc(path, h.corsMiddleware(handler))
}

func (h *Handler) registerProtectedRoute(mux *http.ServeMux, path string, handler http.HandlerFunc) {
	mux.HandleFunc(path, h.corsMiddleware(h.authMiddleware(handler)))
}

const maxAPIErrorLogBytes = 2048

type apiLogResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newAPILogResponseWriter(w http.ResponseWriter) *apiLogResponseWriter {
	return &apiLogResponseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (w *apiLogResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiLogResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.status >= 400 && w.body.Len() < maxAPIErrorLogBytes {
		remaining := maxAPIErrorLogBytes - w.body.Len()
		if len(p) > remaining {
			_, _ = w.body.Write(p[:remaining])
		} else {
			_, _ = w.body.Write(p)
		}
	}
	return w.ResponseWriter.Write(p)
}

func (w *apiLogResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
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

		wrapped := newAPILogResponseWriter(w)
		next(wrapped, r)

		if wrapped.status < 400 || wrapped.status == http.StatusUnauthorized {
			return
		}

		var logf func(format string, args ...interface{})
		if wrapped.status >= 500 {
			logf = logging.Errorf
		} else {
			logf = logging.Warnf
		}

		errMsg := ""
		if wrapped.body.Len() > 0 {
			var resp Response
			if err := json.Unmarshal(wrapped.body.Bytes(), &resp); err == nil && strings.TrimSpace(resp.Error) != "" {
				errMsg = resp.Error
			} else {
				errMsg = strings.TrimSpace(string(wrapped.body.Bytes()))
			}
		}

		if errMsg == "" {
			logf("API error: method=%s path=%s status=%d", r.Method, r.URL.Path, wrapped.status)
			return
		}
		logf("API error: method=%s path=%s status=%d error=%s", r.Method, r.URL.Path, wrapped.status, logging.Truncate(errMsg, 500))
	}
}

// handleHealth 健康检查
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "OK"})
}

func (h *Handler) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
