package api

import (
	"cornerstone/logging"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

type NapCatSettingsRequest struct {
	Enabled               bool     `json:"enabled"`
	AccessToken           string   `json:"access_token,omitempty"`
	ClearAccessToken      bool     `json:"clear_access_token,omitempty"`
	PromptID              string   `json:"prompt_id,omitempty"`
	AllowPrivate          bool     `json:"allow_private"`
	SourceFilterMode      string   `json:"source_filter_mode,omitempty"`
	AllowedPrivateUserIDs []string `json:"allowed_private_user_ids,omitempty"`
}

func (h *Handler) handleNapCatSettings(w http.ResponseWriter, r *http.Request) {
	if h.napCatService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "NapCat service not configured"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := h.napCatService.GetSettings()
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: settings})
		return

	case http.MethodPut:
		var req NapCatSettingsRequest
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

		cfg := h.configManager.GetNapCatConfig()
		cfg.Enabled = req.Enabled
		cfg.PromptID = promptID
		cfg.AllowPrivate = req.AllowPrivate
		cfg.SourceFilterMode = strings.TrimSpace(req.SourceFilterMode)

		if req.AllowedPrivateUserIDs != nil {
			cfg.AllowedPrivateUserIDs = req.AllowedPrivateUserIDs
		}

		switch {
		case req.ClearAccessToken:
			cfg.AccessToken = ""
		case strings.TrimSpace(req.AccessToken) != "" && !strings.Contains(req.AccessToken, "*"):
			cfg.AccessToken = strings.TrimSpace(req.AccessToken)
		}

		if err := h.configManager.UpdateNapCatConfig(cfg); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.napCatService.ApplyCurrentConfig()

		settings, err := h.napCatService.GetSettings()
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: settings})
		return

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
}

func extractNapCatAccessToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		parts := strings.Fields(header)
		if len(parts) == 1 {
			return strings.TrimSpace(parts[0])
		}
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}

	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}

func (h *Handler) handleNapCatWS(w http.ResponseWriter, r *http.Request) {
	if h.napCatService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "NapCat service not configured"})
		return
	}
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	cfg := h.configManager.GetNapCatConfig()
	if !cfg.Enabled {
		h.jsonResponse(w, http.StatusForbidden, Response{Success: false, Error: "NapCat is disabled"})
		return
	}

	expected := strings.TrimSpace(cfg.AccessToken)
	if expected == "" {
		h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "NapCat access token not configured"})
		return
	}

	got := extractNapCatAccessToken(r)
	if got == "" || got != expected {
		h.jsonResponse(w, http.StatusUnauthorized, Response{Success: false, Error: "Unauthorized"})
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, errUpgrade := upgrader.Upgrade(w, r, nil)
	if errUpgrade != nil {
		logging.Warnf("napcat ws upgrade failed: err=%v", errUpgrade)
		return
	}

	h.napCatService.Connect(conn, expected)
}
