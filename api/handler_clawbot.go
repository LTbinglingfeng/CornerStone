package api

import (
	"context"
	"cornerstone/config"
	"net/http"
	"strings"
	"time"
)

type ClawBotSettingsRequest struct {
	Enabled            bool            `json:"enabled"`
	BaseURL            string          `json:"base_url"`
	BotToken           string          `json:"bot_token,omitempty"`
	PromptID           string          `json:"prompt_id,omitempty"`
	ClearBotToken      bool            `json:"clear_bot_token,omitempty"`
	CommandPermissions map[string]bool `json:"command_permissions,omitempty"`
}

type ClawBotQRCodeStartRequest struct {
	BaseURL string `json:"base_url,omitempty"`
}

type ClawBotQRCodePollRequest struct {
	SessionID  string `json:"session_id"`
	VerifyCode string `json:"verify_code,omitempty"`
}

func (h *Handler) handleClawBotSettings(w http.ResponseWriter, r *http.Request) {
	if h.clawBotService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "ClawBot service not configured"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.clawBotService.GetSettings()
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: settings})

	case http.MethodPut:
		var req ClawBotSettingsRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		promptID := strings.TrimSpace(req.PromptID)
		if promptID != "" {
			if _, ok := h.promptManager.Get(promptID); !ok {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt not found"})
				return
			}
		}

		h.clawBotService.credentialMu.Lock()
		defer h.clawBotService.credentialMu.Unlock()
		cfg := h.configManager.GetClawBotConfig()
		previousAccount := clawBotAccount(cfg)
		cfg.Enabled = req.Enabled
		cfg.BaseURL = strings.TrimSpace(req.BaseURL)
		if cfg.BaseURL == "" {
			cfg.BaseURL = config.DefaultClawBotBaseURL
		}
		if err := validateClawBotBaseURL(cfg.BaseURL); err != nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
			return
		}
		cfg.PromptID = promptID
		if req.CommandPermissions != nil {
			cfg.CommandPermissions = config.NormalizeClawBotCommandPermissions(req.CommandPermissions)
		}

		switch {
		case req.ClearBotToken:
			cfg.BotToken = ""
			cfg.ILinkUserID = ""
			cfg.ILinkBotID = ""
			cfg.GetUpdatesBuf = ""
		case strings.TrimSpace(req.BotToken) != "" && strings.TrimSpace(req.BotToken) != strings.TrimSpace(cfg.BotToken):
			cfg.BotToken = strings.TrimSpace(req.BotToken)
			cfg.ILinkUserID = ""
			cfg.ILinkBotID = ""
			cfg.GetUpdatesBuf = ""
		}

		if clawBotAccount(cfg) != previousAccount {
			cfg.GetUpdatesBuf = ""
		}
		if err := h.clawBotService.replaceClawBotConfigLocked(cfg); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		if clawBotAccount(cfg) != previousAccount || req.ClearBotToken || strings.TrimSpace(req.BotToken) != "" {
			h.clawBotService.invalidateQRCodeLoginsLocked()
		}
		h.clawBotService.ApplyCurrentConfig()

		settings, err := h.clawBotService.GetSettings()
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: settings})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleClawBotQRCodeStart(w http.ResponseWriter, r *http.Request) {
	if h.clawBotService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "ClawBot service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req ClawBotQRCodeStartRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	resp, err := h.clawBotService.StartQRCode(ctx, req.BaseURL)
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: resp})
}

func (h *Handler) handleClawBotQRCodePoll(w http.ResponseWriter, r *http.Request) {
	if h.clawBotService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "ClawBot service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req ClawBotQRCodePollRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()

	if !validClawBotVerifyCode(strings.TrimSpace(req.VerifyCode)) {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid verification code"})
		return
	}
	resp, err := h.clawBotService.PollQRCode(ctx, strings.TrimSpace(req.SessionID), strings.TrimSpace(req.VerifyCode))
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: resp})
}
