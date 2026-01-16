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

