package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type qqOfficialLogin struct {
	config                 config.QQOfficialConfig
	id, status, appID, err string
	binding                *client.QQOfficialBinding
	expires, nextPoll      time.Time
	polling                bool
	generation             uint64
	ctx                    context.Context
	cancel                 context.CancelFunc
}

func (s *QQOfficialService) settingsLocked() map[string]interface{} {
	c := s.handler.configManager.GetQQOfficialConfig()
	status := s.status.Status
	if !c.Enabled {
		status = "disabled"
	} else if strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.AppSecret) == "" {
		status = "missing_credentials"
	}
	promptName := ""
	if s.handler.promptManager != nil {
		if p, ok := s.handler.promptManager.Get(c.PromptID); ok {
			promptName = p.Name
		}
	}
	return map[string]interface{}{"command_permissions": c.CommandPermissions, "revision": fmt.Sprint(s.generation), "prompt_name": promptName, "enabled": c.Enabled, "app_id": c.AppID, "has_secret": c.AppSecret != "", "prompt_id": c.PromptID, "source_filter_mode": c.SourceFilterMode, "allowed_user_openids": c.AllowedUserOpenIDs, "status": status, "last_error": s.status.LastError, "bot_id": s.status.BotID, "bot_name": s.status.BotName}
}
func (h *Handler) handleQQOfficialSettings(w http.ResponseWriter, r *http.Request) {
	s := h.qqOfficialService
	if s == nil {
		h.jsonResponse(w, 503, Response{Error: "QQ Official unavailable"})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		h.jsonResponse(w, 405, Response{Error: "Method not allowed"})
		return
	}
	var req struct {
		config.QQOfficialConfig
		ClearSecret bool    `json:"clear_secret"`
		Revision    *string `json:"revision"`
	}
	if r.Method == http.MethodPut && !h.decodeJSON(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodPut {
		// Optional for legacy clients; current forms always submit their snapshot revision.
		if req.Revision != nil && *req.Revision != fmt.Sprint(s.generation) {
			h.jsonResponse(w, http.StatusConflict, Response{Error: "QQ Official configuration changed; reload before saving"})
			return
		}
		c := req.QQOfficialConfig
		previous := h.configManager.GetQQOfficialConfig()
		if c.CommandPermissions == nil {
			c.CommandPermissions = previous.CommandPermissions
		}
		if c.PromptID != "" {
			if h.promptManager == nil {
				h.jsonResponse(w, 400, Response{Error: "Prompt not found"})
				return
			}
			if _, ok := h.promptManager.Get(strings.TrimSpace(c.PromptID)); !ok {
				h.jsonResponse(w, 400, Response{Error: "Prompt not found"})
				return
			}
		}
		if strings.TrimSpace(c.AppSecret) == "" {
			// A secret belongs to one AppID, never carry it to another account.
			c.AppSecret = ""
			if strings.TrimSpace(c.AppID) == previous.AppID {
				c.AppSecret = previous.AppSecret
			}
		}
		if req.ClearSecret {
			c.AppSecret = ""
		}
		if err := h.configManager.UpdateQQOfficialConfig(c); err != nil {
			h.jsonResponse(w, 400, Response{Error: "Unable to save QQ Official configuration"})
			return
		}
		s.applyLocked()
	}
	h.jsonResponse(w, 200, Response{Success: true, Data: s.settingsLocked()})
}
func (s *QQOfficialService) startLogin() (map[string]interface{}, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("service closed")
	}
	if s.login != nil {
		s.login.cancel()
		s.login.binding = nil
		s.login.status = "cancelled"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	l := &qqOfficialLogin{id: hex.EncodeToString(bytes[:]), status: "pending", ctx: ctx, cancel: cancel, generation: s.generation, expires: time.Now().Add(5 * time.Minute)}
	l.config = s.handler.configManager.GetQQOfficialConfig()
	s.login = l
	s.mu.Unlock()
	binding, err := s.startBinding(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.login != l || ctx.Err() != nil || l.generation != s.generation {
		cancel()
		return nil, fmt.Errorf("login cancelled")
	}
	if err != nil || binding == nil {
		cancel()
		l.status = "error"
		l.err = "Unable to start binding"
		return nil, fmt.Errorf("unable to start binding")
	}
	if binding.ExpiresAt.Before(l.expires) {
		l.expires = binding.ExpiresAt
	}
	if !l.expires.After(time.Now()) {
		cancel()
		l.status = "expired"
		return nil, fmt.Errorf("binding expired")
	}
	l.binding = binding
	expiryDelay := time.Until(l.expires)
	go func() {
		timer := time.NewTimer(expiryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-l.ctx.Done():
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if l.status == "pending" {
			l.status = "expired"
			l.binding = nil
			l.cancel()
		}
	}()
	return map[string]interface{}{"id": l.id, "qr_url": binding.URL, "expires_at": l.expires.Format(time.RFC3339), "interval_seconds": 2, "status": "pending"}, nil
}
func (l *qqOfficialLogin) response() map[string]interface{} {
	m := map[string]interface{}{"id": l.id, "status": l.status}
	if l.appID != "" {
		m["app_id"] = l.appID
	}
	if l.err != "" {
		m["error"] = l.err
	}
	return m
}
func (s *QQOfficialService) loginStatus(id string, cancel bool) (map[string]interface{}, error) {
	s.mu.Lock()
	l := s.login
	if l == nil || id == "" || id != l.id {
		s.mu.Unlock()
		return nil, fmt.Errorf("login not found")
	}
	if cancel && l.status == "pending" {
		l.cancel()
		l.status = "cancelled"
		l.binding = nil
	}
	if l.status == "pending" && (time.Now().After(l.expires) || l.ctx.Err() != nil) {
		l.cancel()
		l.status = "expired"
		l.binding = nil
	}
	if l.status != "pending" || l.polling || time.Now().Before(l.nextPoll) || l.binding == nil {
		result := l.response()
		s.mu.Unlock()
		return result, nil
	}
	l.polling = true
	l.nextPoll = time.Now().Add(2 * time.Second)
	binding := l.binding
	ctx, stop := context.WithTimeout(l.ctx, 15*time.Second)
	s.mu.Unlock()
	result, err := s.pollBinding(ctx, binding)
	stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	l.polling = false
	if !reflect.DeepEqual(l.config, s.handler.configManager.GetQQOfficialConfig()) && l.status == "pending" {
		l.status = "cancelled"
		l.cancel()
		l.binding = nil
	}
	if s.login != l || l.status != "pending" || l.generation != s.generation || s.closed {
		return l.response(), nil
	}
	if time.Now().After(l.expires) || l.ctx.Err() != nil {
		l.cancel()
		l.status = "expired"
		l.binding = nil
		return l.response(), nil
	}
	if err != nil {
		l.err = "Unable to poll binding"
		return l.response(), nil
	}
	l.err = ""
	if result == nil {
		return l.response(), nil
	}
	switch result.Status {
	case "expired":
		l.status = "expired"
		l.cancel()
		l.binding = nil
	case "created":
		// Service lock serializes manual settings and QR completion. Preserve all other settings.
		c := s.handler.configManager.GetQQOfficialConfig()
		c.AppID = result.AppID
		c.AppSecret = result.AppSecret
		c.Enabled = true
		if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
			l.status = "error"
			l.err = "Unable to save binding"
			l.cancel()
			l.binding = nil
			break
		}
		s.applyLocked()
		l.status = "created"
		l.appID = c.AppID
		l.binding = nil
	}
	return l.response(), nil
}
func (h *Handler) handleQQOfficialLogin(w http.ResponseWriter, r *http.Request) {
	s := h.qqOfficialService
	if s == nil {
		h.jsonResponse(w, 503, Response{Error: "QQ Official unavailable"})
		return
	}
	var data map[string]interface{}
	var err error
	switch r.URL.Path {
	case "/api/channel/qq-official/login/start":
		if r.Method != "POST" {
			h.jsonResponse(w, 405, Response{Error: "Method not allowed"})
			return
		}
		data, err = s.startLogin()
	case "/api/channel/qq-official/login/status":
		if r.Method != "GET" {
			h.jsonResponse(w, 405, Response{Error: "Method not allowed"})
			return
		}
		data, err = s.loginStatus(r.URL.Query().Get("id"), false)
	case "/api/channel/qq-official/login/cancel":
		if r.Method != "POST" {
			h.jsonResponse(w, 405, Response{Error: "Method not allowed"})
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if !h.decodeJSON(w, r, &req) {
			return
		}
		data, err = s.loginStatus(req.ID, true)
	}
	if err != nil {
		h.jsonResponse(w, 400, Response{Error: err.Error()})
		return
	}
	h.jsonResponse(w, 200, Response{Success: true, Data: data})
}
