package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AuthStatus struct {
	NeedsSetup    bool   `json:"needs_setup"`
	Username      string `json:"username,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	Authenticated bool   `json:"authenticated"`
}

type AuthSession struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	UserID   string `json:"user_id"`
}

type AuthSetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authToken struct {
	username  string
	userID    string
	createdAt time.Time
}

type authTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]authToken
}

const authTokenCookieName = "cornerstone_auth_token"

func newAuthTokenStore() *authTokenStore {
	return &authTokenStore{
		tokens: make(map[string]authToken),
	}
}

func (s *authTokenStore) create(username, userID string) (string, error) {
	token, err := generateAuthToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.tokens[token] = authToken{
		username:  username,
		userID:    userID,
		createdAt: time.Now(),
	}
	s.mu.Unlock()
	return token, nil
}

func (s *authTokenStore) validate(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	_, ok := s.tokens[token]
	s.mu.RUnlock()
	return ok
}

func generateAuthToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func extractAuthToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		parts := strings.Fields(header)
		if len(parts) == 1 {
			return parts[0]
		}
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1]
		}
	}

	cookie, errCookie := r.Cookie(authTokenCookieName)
	if errCookie == nil {
		token := strings.TrimSpace(cookie.Value)
		if token != "" {
			return token
		}
	}

	return ""
}

func setAuthTokenCookie(w http.ResponseWriter, r *http.Request, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	})
}

func normalizeHost(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed != nil && strings.TrimSpace(parsed.Host) != "" {
			trimmed = strings.TrimSpace(parsed.Host)
		}
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil && strings.TrimSpace(host) != "" {
		trimmed = strings.TrimSpace(host)
	}
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	return strings.ToLower(strings.TrimSpace(trimmed))
}

func isLoopbackHost(value string) bool {
	host := normalizeHost(value)
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !isLoopbackHost(r.RemoteAddr) {
		return false
	}
	if !forwardedLoopbackOnly(r.Header.Values("X-Forwarded-For")) {
		return false
	}
	if !forwardedLoopbackOnly(r.Header.Values("X-Real-IP")) {
		return false
	}
	return true
}

func forwardedLoopbackOnly(values []string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if !isLoopbackHost(trimmed) {
				return false
			}
		}
	}
	return true
}

func isTrustedBootstrapOrigin(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Host) == "" {
		return false
	}

	originHost := strings.ToLower(strings.TrimSpace(parsed.Host))
	requestHost := strings.ToLower(strings.TrimSpace(r.Host))
	if originHost == requestHost {
		return true
	}

	return isLoopbackHost(originHost) && isLoopbackHost(requestHost)
}

func (h *Handler) requireLocalBootstrapAccess(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.authManager == nil || h.authManager.IsSetup() {
		return true
	}
	if !isLoopbackRequest(r) {
		h.jsonResponse(w, http.StatusForbidden, Response{Success: false, Error: "Initial setup is only available from localhost"})
		return false
	}
	if !isTrustedBootstrapOrigin(r) {
		h.jsonResponse(w, http.StatusForbidden, Response{Success: false, Error: "Initial setup must be performed from the local Web UI"})
		return false
	}
	return true
}

func (h *Handler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authManager == nil || h.tokenStore == nil {
			next(w, r)
			return
		}
		if !h.authManager.IsSetup() {
			logging.Warnf("auth failed: path=%s ip=%s reason=%s", r.URL.Path, r.RemoteAddr, "auth_not_set")
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Authentication not set"})
			return
		}
		token := extractAuthToken(r)
		if token == "" {
			logging.Warnf("auth failed: path=%s ip=%s reason=%s", r.URL.Path, r.RemoteAddr, "missing_token")
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Unauthorized"})
			return
		}
		if !h.tokenStore.validate(token) {
			logging.Warnf("auth failed: path=%s ip=%s reason=%s", r.URL.Path, r.RemoteAddr, "invalid_token")
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Unauthorized"})
			return
		}
		next(w, r)
	}
}

func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	if !h.requireLocalBootstrapAccess(w, r) {
		return
	}

	if h.authManager == nil {
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: AuthStatus{NeedsSetup: false, Authenticated: true}})
		return
	}

	info := h.authManager.GetInfo()
	if info == nil {
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: AuthStatus{NeedsSetup: true}})
		return
	}

	token := extractAuthToken(r)
	authenticated := false
	if h.tokenStore != nil {
		authenticated = h.tokenStore.validate(token)
		if authenticated {
			setAuthTokenCookie(w, r, token)
		}
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: AuthStatus{
		NeedsSetup:    false,
		Username:      info.Username,
		UserID:        info.UserID,
		Authenticated: authenticated,
	}})
}

func (h *Handler) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	if !h.requireLocalBootstrapAccess(w, r) {
		return
	}

	if h.authManager == nil || h.tokenStore == nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Auth manager not configured"})
		return
	}

	var req AuthSetupRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Username and password required"})
		return
	}

	info, err := h.authManager.Setup(username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrAuthExists):
			h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Auth already initialized"})
		case errors.Is(err, storage.ErrInvalidAuthUsername), errors.Is(err, storage.ErrInvalidAuthPassword):
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
		default:
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		}
		return
	}

	if h.userManager != nil {
		if _, err := h.userManager.Update(info.Username, ""); err != nil {
			logging.Errorf("sync username failed: %v", err)
		}
	}

	token, err := h.tokenStore.create(info.Username, info.UserID)
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}

	setAuthTokenCookie(w, r, token)
	h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: AuthSession{
		Token:    token,
		Username: info.Username,
		UserID:   info.UserID,
	}})
}

func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	if h.authManager == nil || h.tokenStore == nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Auth manager not configured"})
		return
	}

	var req AuthLoginRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	if req.Password == "" {
		logging.Warnf("login failed: username=%s ip=%s reason=%s", username, r.RemoteAddr, "missing_password")
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Password required"})
		return
	}

	info, err := h.authManager.Verify(username, req.Password)
	if err != nil {
		reason := "internal_error"
		switch {
		case errors.Is(err, storage.ErrAuthNotSetup):
			reason = "auth_not_setup"
			h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Auth not setup"})
		case errors.Is(err, storage.ErrInvalidCredentials):
			reason = "invalid_credentials"
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Invalid credentials"})
		default:
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		}
		logging.Warnf("login failed: username=%s ip=%s reason=%s", username, r.RemoteAddr, reason)
		return
	}

	token, err := h.tokenStore.create(info.Username, info.UserID)
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}

	logging.Infof("login success: username=%s ip=%s", info.Username, r.RemoteAddr)
	setAuthTokenCookie(w, r, token)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: AuthSession{
		Token:    token,
		Username: info.Username,
		UserID:   info.UserID,
	}})
}
