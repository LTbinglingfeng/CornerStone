package api

import (
	"cornerstone/config"
	"encoding/json"
	"net/http"
	"strings"
)

type ttsSettingsUpdateRequest struct {
	Enabled  *bool           `json:"enabled,omitempty"`
	Provider json.RawMessage `json:"provider,omitempty"`
}

func maskAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	if len(apiKey) > 8 {
		return apiKey[:4] + "****" + apiKey[len(apiKey)-4:]
	}
	return "****"
}

func (h *Handler) handleTTSSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.configManager.Get()
		var provider *config.TTSProvider
		if cfg.TTSProvider != nil {
			clone := *cfg.TTSProvider
			clone.APIKey = maskAPIKey(clone.APIKey)
			provider = &clone
		}
		h.jsonResponse(w, http.StatusOK, Response{
			Success: true,
			Data: map[string]interface{}{
				"enabled":  cfg.TTSEnabled,
				"provider": provider,
			},
		})
		return

	case http.MethodPut:
		var req ttsSettingsUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		cfg := h.configManager.Get()

		if req.Enabled != nil {
			cfg.TTSEnabled = *req.Enabled
		}

		if req.Provider != nil {
			if string(req.Provider) == "null" {
				cfg.TTSProvider = nil
			} else {
				var incoming config.TTSProvider
				if errUnmarshal := json.Unmarshal(req.Provider, &incoming); errUnmarshal != nil {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid provider"})
					return
				}

				incoming.Type = config.TTSProviderType(strings.TrimSpace(string(incoming.Type)))
				if incoming.Type == "" {
					incoming.Type = config.TTSProviderTypeMinimax
				}
				if incoming.Type != config.TTSProviderTypeMinimax {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Unsupported TTS provider"})
					return
				}

				existing := cfg.TTSProvider

				apiKey := strings.TrimSpace(incoming.APIKey)
				if existing != nil && (apiKey == "" || strings.Contains(apiKey, "*")) {
					apiKey = existing.APIKey
				} else if strings.Contains(apiKey, "*") {
					apiKey = ""
				}
				apiKey = strings.TrimSpace(apiKey)
				if apiKey == "" {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "API key is required"})
					return
				}

				baseURL := strings.TrimSpace(incoming.BaseURL)
				if baseURL == "" {
					if existing != nil {
						baseURL = strings.TrimSpace(existing.BaseURL)
					} else {
						baseURL = "https://api.minimaxi.com"
					}
				}
				baseURL = strings.TrimRight(baseURL, "/")

				model := strings.TrimSpace(incoming.Model)
				if model == "" {
					if existing != nil {
						model = strings.TrimSpace(existing.Model)
					} else {
						model = "speech-2.6-hd"
					}
				}

				voiceID := strings.TrimSpace(incoming.VoiceSetting.VoiceID)
				if voiceID == "" {
					if existing != nil {
						voiceID = strings.TrimSpace(existing.VoiceSetting.VoiceID)
					} else {
						voiceID = "male-qn-qingse"
					}
				}

				speed := incoming.VoiceSetting.Speed
				if speed == 0 {
					if existing != nil && existing.VoiceSetting.Speed != 0 {
						speed = existing.VoiceSetting.Speed
					} else {
						speed = 1
					}
				}
				if speed < 0.5 {
					speed = 0.5
				}
				if speed > 2 {
					speed = 2
				}

				languageBoost := strings.TrimSpace(incoming.LanguageBoost)

				cfg.TTSProvider = &config.TTSProvider{
					Type:    incoming.Type,
					BaseURL: baseURL,
					APIKey:  apiKey,
					Model:   model,
					VoiceSetting: config.TTSVoiceSetting{
						VoiceID: voiceID,
						Speed:   speed,
					},
					LanguageBoost: languageBoost,
				}
			}
		}

		if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
			return
		}

		var provider *config.TTSProvider
		if cfg.TTSProvider != nil {
			clone := *cfg.TTSProvider
			clone.APIKey = maskAPIKey(clone.APIKey)
			provider = &clone
		}

		h.jsonResponse(w, http.StatusOK, Response{
			Success: true,
			Data: map[string]interface{}{
				"enabled":  cfg.TTSEnabled,
				"provider": provider,
			},
		})
		return

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
}
