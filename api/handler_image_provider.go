package api

import (
	"cornerstone/config"
	"net/http"
	"strings"
)

func (h *Handler) handleImageProvider(w http.ResponseWriter, r *http.Request) {
	if h.configManager == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Config manager not configured"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg := h.configManager.Get()
		h.jsonResponse(w, http.StatusOK, Response{
			Success: true,
			Data:    map[string]interface{}{"image_provider_id": cfg.ImageProviderID},
		})

	case http.MethodPut, http.MethodPost:
		var req SetActiveProviderRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		providerID := strings.TrimSpace(req.ProviderID)
		if err := h.configManager.SetImageProvider(providerID); err != nil {
			if err == config.ErrProviderNotFound {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: err.Error()})
				return
			}
			if err == config.ErrProviderNotImageGenCapable {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: err.Error()})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{
			Success: true,
			Data:    map[string]interface{}{"image_provider_id": providerID},
		})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}
