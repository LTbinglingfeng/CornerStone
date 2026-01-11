package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
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
	if header == "" {
		queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
		if queryToken != "" {
			return queryToken
		}
		return ""
	}
	parts := strings.Fields(header)
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func (h *Handler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authManager == nil || h.tokenStore == nil {
			next(w, r)
			return
		}
		if !h.authManager.IsSetup() {
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Authentication not set"})
			return
		}
		token := extractAuthToken(r)
		if !h.tokenStore.validate(token) {
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
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Password required"})
		return
	}

	info, err := h.authManager.Verify(username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrAuthNotSetup):
			h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Auth not setup"})
		case errors.Is(err, storage.ErrInvalidCredentials):
			h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Invalid credentials"})
		default:
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		}
		return
	}

	token, err := h.tokenStore.create(info.Username, info.UserID)
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: AuthSession{
		Token:    token,
		Username: info.Username,
		UserID:   info.UserID,
	}})
}
