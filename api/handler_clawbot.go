package api

import (
	"context"
	"cornerstone/config"
	"net/http"
	"strings"
	"time"
)

type ClawBotSettingsRequest struct {
	Enabled       bool   `json:"enabled"`
	BaseURL       string `json:"base_url"`
	BotToken      string `json:"bot_token,omitempty"`
	PromptID      string `json:"prompt_id,omitempty"`
	ClearBotToken bool   `json:"clear_bot_token,omitempty"`
}

type ClawBotQRCodeStartRequest struct {
	BaseURL string `json:"base_url,omitempty"`
}

type ClawBotQRCodePollRequest struct {
	SessionID string `json:"session_id"`
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

		cfg := h.configManager.GetClawBotConfig()
		cfg.Enabled = req.Enabled
		cfg.BaseURL = strings.TrimSpace(req.BaseURL)
		if cfg.BaseURL == "" {
			cfg.BaseURL = config.DefaultClawBotBaseURL
		}
		cfg.PromptID = promptID

		switch {
		case req.ClearBotToken:
			cfg.BotToken = ""
			cfg.ILinkUserID = ""
			cfg.GetUpdatesBuf = ""
		case strings.TrimSpace(req.BotToken) != "":
			cfg.BotToken = strings.TrimSpace(req.BotToken)
			cfg.GetUpdatesBuf = ""
		}

		if err := h.configManager.UpdateClawBotConfig(cfg); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.clawBotService.PollQRCode(ctx, strings.TrimSpace(req.SessionID))
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
		return
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: resp})
}
