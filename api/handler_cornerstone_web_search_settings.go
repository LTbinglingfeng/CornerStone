package api

import (
	"cornerstone/config"
	"cornerstone/internal/search"
	"cornerstone/internal/search/providers"
	"net/http"
	"strings"
)

type cornerstoneWebSearchProviderPatch struct {
	APIKey            *string `json:"api_key,omitempty"`
	APIHost           *string `json:"api_host,omitempty"`
	SearchEngine      *string `json:"search_engine,omitempty"`
	BasicAuthUsername *string `json:"basic_auth_username,omitempty"`
	BasicAuthPassword *string `json:"basic_auth_password,omitempty"`
}

type cornerstoneWebSearchSettingsPatch struct {
	ActiveProviderID *string                                      `json:"active_provider_id,omitempty"`
	MaxResults       *int                                         `json:"max_results,omitempty"`
	FetchResults     *int                                         `json:"fetch_results,omitempty"`
	ExcludeDomains   *[]string                                    `json:"exclude_domains,omitempty"`
	SearchWithTime   *bool                                        `json:"search_with_time,omitempty"`
	TimeoutSeconds   *int                                         `json:"timeout_seconds,omitempty"`
	Providers        map[string]cornerstoneWebSearchProviderPatch `json:"providers,omitempty"`
}

func (h *Handler) handleCornerstoneWebSearchSettings(w http.ResponseWriter, r *http.Request) {
	if h.configManager == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Config manager not configured"})
		return
	}

	reg := search.NewRegistry()
	if errRegister := providers.RegisterAll(reg); errRegister != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errRegister.Error()})
		return
	}
	availableProviders := reg.Infos(nil)
	supported := make(map[string]struct{}, len(availableProviders))
	for _, info := range availableProviders {
		id := strings.ToLower(strings.TrimSpace(info.ID))
		if id == "" {
			continue
		}
		supported[id] = struct{}{}
	}

	switch r.Method {
	case http.MethodGet:
		cfg := h.configManager.Get()
		settings := cfg.CornerstoneWebSearch
		maskedProviders := make(map[string]config.WebSearchProvider, len(settings.Providers))
		for id, providerCfg := range settings.Providers {
			copyCfg := providerCfg
			if len(copyCfg.APIKey) > 8 {
				copyCfg.APIKey = copyCfg.APIKey[:4] + "****" + copyCfg.APIKey[len(copyCfg.APIKey)-4:]
			} else if len(copyCfg.APIKey) > 0 {
				copyCfg.APIKey = "****"
			}
			if strings.TrimSpace(copyCfg.BasicAuthPassword) != "" {
				copyCfg.BasicAuthPassword = "****"
			}
			maskedProviders[id] = copyCfg
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{
			"active_provider_id":  settings.ActiveProviderID,
			"providers":           maskedProviders,
			"max_results":         settings.MaxResults,
			"fetch_results":       settings.FetchResults,
			"exclude_domains":     settings.ExcludeDomains,
			"search_with_time":    settings.SearchWithTime,
			"timeout_seconds":     settings.TimeoutSeconds,
			"available_providers": availableProviders,
		}})
		return

	case http.MethodPut:
		var req cornerstoneWebSearchSettingsPatch
		if !h.decodeJSON(w, r, &req) {
			return
		}

		cfg := h.configManager.Get()
		settings := cfg.CornerstoneWebSearch

		if req.ActiveProviderID != nil {
			id := strings.ToLower(strings.TrimSpace(*req.ActiveProviderID))
			if id != "" {
				if _, ok := supported[id]; !ok {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Unsupported provider"})
					return
				}
			}
			settings.ActiveProviderID = id
		}
		if req.MaxResults != nil {
			settings.MaxResults = *req.MaxResults
			// Backwards compatibility: older clients only update max_results. Before fetch_results
			// existed, the upstream fetch count effectively followed max_results. Preserve that
			// behavior unless the caller explicitly sets fetch_results.
			if req.FetchResults == nil {
				settings.FetchResults = settings.MaxResults
			}
		}
		if req.FetchResults != nil {
			settings.FetchResults = *req.FetchResults
		}
		if req.ExcludeDomains != nil {
			settings.ExcludeDomains = *req.ExcludeDomains
		}
		if req.SearchWithTime != nil {
			settings.SearchWithTime = *req.SearchWithTime
		}
		if req.TimeoutSeconds != nil {
			settings.TimeoutSeconds = *req.TimeoutSeconds
		}
		if req.Providers != nil {
			// Clone the providers map before mutating it to avoid concurrent map read/write panics.
			clonedProviders := make(map[string]config.WebSearchProvider, len(settings.Providers))
			for id, providerCfg := range settings.Providers {
				clonedProviders[id] = providerCfg
			}
			settings.Providers = clonedProviders

			for rawID, patch := range req.Providers {
				id := strings.ToLower(strings.TrimSpace(rawID))
				if id == "" {
					continue
				}
				if _, ok := supported[id]; !ok {
					h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Unsupported provider"})
					return
				}
				current := settings.Providers[id]
				if patch.APIKey != nil {
					current.APIKey = strings.TrimSpace(*patch.APIKey)
				}
				if patch.APIHost != nil {
					current.APIHost = strings.TrimSpace(*patch.APIHost)
				}
				if patch.SearchEngine != nil {
					if id == providers.ProviderIDZhipu {
						value := strings.TrimSpace(*patch.SearchEngine)
						if value != "" && !providers.IsValidZhipuSearchEngine(value) {
							h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Unsupported Zhipu search engine"})
							return
						}
						current.SearchEngine = providers.NormalizeZhipuSearchEngine(value)
					} else {
						current.SearchEngine = ""
					}
				}
				if patch.BasicAuthUsername != nil {
					current.BasicAuthUsername = strings.TrimSpace(*patch.BasicAuthUsername)
				}
				if patch.BasicAuthPassword != nil {
					current.BasicAuthPassword = strings.TrimSpace(*patch.BasicAuthPassword)
				}
				settings.Providers[id] = current
			}
		}

		cfg.CornerstoneWebSearch = settings
		if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"ok": true}})
		return

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
}
