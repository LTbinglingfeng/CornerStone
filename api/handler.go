package api

import (
	"bufio"
	"cornerstone/config"
	"cornerstone/exacttime"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"net"
	"net/http"
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
	ttsAudioDir   string

	memoryManager   *storage.MemoryManager
	memoryExtractor *storage.MemoryExtractor
	memorySessions  map[string]*storage.MemorySession
	sessionsMu      sync.RWMutex

	clawBotService      *ClawBotService
	qqOfficialService   *QQOfficialService
	napCatService       *NapCatService
	reminderService     *ReminderService
	idleGreetingService *IdleGreetingService
	weatherService      weatherService
	weatherServiceOnce  sync.Once
	exactTimeService    exactTimeProvider

	cleanupOnce sync.Once
	cleanupDone chan struct{}
}

type exactTimeProvider interface {
	Now() time.Time
	Status() exacttime.Status
}

// NewHandler 创建处理器
func NewHandler(cm *config.Manager, pm *storage.PromptManager, chatMgr *storage.ChatManager, userMgr *storage.UserManager, authMgr *storage.AuthManager, cachePhotoDir string, ttsAudioDir string, memoryManager *storage.MemoryManager, memoryExtractor *storage.MemoryExtractor) *Handler {
	return &Handler{
		configManager:   cm,
		promptManager:   pm,
		chatManager:     chatMgr,
		userManager:     userMgr,
		authManager:     authMgr,
		tokenStore:      newAuthTokenStore(),
		cachePhotoDir:   cachePhotoDir,
		ttsAudioDir:     ttsAudioDir,
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
	h.registerProtectedRoute(mux, storage.AttachmentURLPrefix, h.handleAttachment)

	// 记忆接口
	h.registerProtectedRoute(mux, "/api/memory/", h.handleMemory)
	h.registerProtectedRoute(mux, "/api/settings/memory-provider", h.handleSetMemoryProvider)
	h.registerProtectedRoute(mux, "/api/settings/memory-enabled", h.handleSetMemoryEnabled)
	h.registerProtectedRoute(mux, "/api/settings/memory-extraction", h.handleMemoryExtractionSettings)
	h.registerProtectedRoute(mux, "/api/settings/memory-extraction-prompt", h.handleMemoryExtractionPrompt)
	h.registerProtectedRoute(mux, "/api/settings/tts", h.handleTTSSettings)
	h.registerProtectedRoute(mux, "/api/settings/clawbot", h.handleClawBotSettings)
	h.registerProtectedRoute(mux, "/api/settings/clawbot/deliveries", h.handleClawBotDeliveries)
	h.registerProtectedRoute(mux, "/api/settings/clawbot/qr-start", h.handleClawBotQRCodeStart)
	h.registerProtectedRoute(mux, "/api/settings/clawbot/qr-poll", h.handleClawBotQRCodePoll)
	h.registerProtectedRoute(mux, "/api/settings/napcat", h.handleNapCatSettings)
	h.registerProtectedRoute(mux, "/api/settings/qq-official", h.handleQQOfficialSettings)
	h.registerProtectedRoute(mux, "/api/channel/qq-official/login/start", h.handleQQOfficialLogin)
	h.registerProtectedRoute(mux, "/api/channel/qq-official/login/status", h.handleQQOfficialLogin)
	h.registerProtectedRoute(mux, "/api/channel/qq-official/login/cancel", h.handleQQOfficialLogin)
	h.registerProtectedRoute(mux, "/api/settings/reminders", h.handleReminders)
	h.registerProtectedRoute(mux, "/api/settings/reminders/", h.handleReminderByID)
	h.registerProtectedRoute(mux, cornerstoneWebSearchSettingsPath, h.handleCornerstoneWebSearchSettings)
	h.registerProtectedRoute(mux, legacyCornerstoneWebSearchSettingsPath, h.handleCornerstoneWebSearchSettings)

	// 运行日志查看
	h.registerProtectedRoute(mux, logsPath, h.handleLogs)
	h.registerProtectedRoute(mux, logsDownloadPath, h.handleLogsDownload)

	// 配置接口 (使用 /management 前缀)
	h.registerProtectedRoute(mux, "/management/config", h.handleConfig)

	// 供应商管理接口
	h.registerProtectedRoute(mux, "/management/providers", h.handleProviders)
	h.registerProtectedRoute(mux, "/management/providers/", h.handleProviderByID)
	h.registerProtectedRoute(mux, "/management/providers/active", h.handleActiveProvider)
	h.registerProtectedRoute(mux, "/management/image-provider", h.handleImageProvider)
	h.registerProtectedRoute(mux, "/management/memory-provider", h.handleMemoryProvider)
	h.registerProtectedRoute(mux, "/management/weather/cities/search", h.handleWeatherCitySearch)

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
	h.registerProtectedRoute(mux, "/management/tts-audio/", h.handleTTSAudioByName)

	// 健康检查
	h.registerPublicRoute(mux, "/management/health", h.handleHealth)

	// NapCat reverse websocket channel (no management auth, uses napcat access_token)
	h.registerPublicRoute(mux, "/api/channel/napcat/ws", h.handleNapCatWS)
}

func (h *Handler) registerPublicRoute(mux *http.ServeMux, path string, handler http.HandlerFunc) {
	mux.HandleFunc(path, h.corsMiddleware(handler))
}

func (h *Handler) registerProtectedRoute(mux *http.ServeMux, path string, handler http.HandlerFunc) {
	mux.HandleFunc(path, h.corsMiddleware(h.authMiddleware(handler)))
}

type apiLogResponseWriter struct {
	http.ResponseWriter
	status       int
	wroteHeader  bool
	bytesWritten int
	writeError   error
	hijacked     bool
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
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	if err != nil {
		w.writeError = err
	}
	return n, err
}

func (w *apiLogResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *apiLogResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacker not supported")
	}
	conn, rw, err := hijacker.Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}

// corsMiddleware 处理跨域请求
func (h *Handler) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Deliberately exclude query strings, headers and response bodies: they can
		// contain credentials (including the NapCat websocket access token).
		ctx := logging.EnsureRequest(r.Context(), "web", "http_request")
		ctx = logging.WithFields(ctx, "method", r.Method, "path", r.URL.Path)
		r = r.WithContext(ctx)
		started := time.Now()
		wrapped := newAPILogResponseWriter(w)
		returned := false
		quiet := r.Method == http.MethodGet && quietRequestLogPaths[r.URL.Path]
		if !quiet {
			logging.Event(ctx, "http.request.started")
		}
		defer func() {
			status := "completed"
			if !returned {
				status = "aborted"
			} else if wrapped.hijacked {
				status = "hijacked"
			} else if wrapped.writeError != nil {
				status = "write_failed"
			} else if r.Context().Err() != nil {
				status = "canceled"
			} else if wrapped.status >= 400 {
				status = "rejected"
			}
			if quiet && (status == "completed" || status == "canceled") {
				return
			}
			logging.Event(ctx, "http.request.completed", "status", status,
				"http_status", wrapped.status, "duration_ms", time.Since(started).Milliseconds(),
				"response_bytes", wrapped.bytesWritten, "write_error", logging.ErrorKind(wrapped.writeError))
		}()
		if r.Method == "OPTIONS" {
			wrapped.WriteHeader(http.StatusOK)
		} else {
			next(wrapped, r)
		}
		returned = true
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

func (h *Handler) SetClawBotService(service *ClawBotService) {
	h.clawBotService = service
}

func (h *Handler) SetNapCatService(service *NapCatService) {
	h.napCatService = service
}

func (h *Handler) SetExactTimeService(service exactTimeProvider) {
	h.exactTimeService = service
}

func (h *Handler) now() time.Time {
	if h == nil || h.exactTimeService == nil {
		return time.Now()
	}
	return h.exactTimeService.Now()
}

func (h *Handler) SetReminderService(service *ReminderService) {
	h.reminderService = service
}

func (h *Handler) SetIdleGreetingService(service *IdleGreetingService) {
	h.idleGreetingService = service
}
