package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"cornerstone/storage"
	"net/http"
	"time"
)

const statisticsPath = "/management/statistics"

// SetStatisticsManager must be called before serving requests or starting workers.
func (h *Handler) SetStatisticsManager(manager *storage.StatisticsManager) {
	h.statisticsManager = manager
}

func (h *Handler) withStatistics(ctx context.Context, providerID string) context.Context {
	if h.statisticsManager == nil {
		return ctx
	}
	return client.WithCallRecorder(ctx, h.statisticsManager, providerID)
}

// handleStatistics returns [from,to) history and an independent live 60s window.
// Both windows use completion timestamps and the same provider/model filters.
func (h *Handler) handleStatistics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	params := r.URL.Query()
	for key, values := range params {
		if len(values) != 1 || (key != "from" && key != "to" && key != "provider_id" && key != "model") {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid statistics query parameter"})
			return
		}
	}
	// Use the same wall clock as client observation, not the NTP-adjusted scheduler.
	now := time.Now().UTC()
	q := storage.StatisticsQuery{To: now, ProviderID: params.Get("provider_id"), Model: params.Get("model")}
	for name, dest := range map[string]*time.Time{"from": &q.From, "to": &q.To} {
		if raw, present := params[name]; present {
			value, err := time.Parse(time.RFC3339Nano, raw[0])
			if err != nil || value.IsZero() {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid " + name + ": expected RFC3339 timestamp"})
				return
			}
			*dest = value.UTC()
		}
	}
	if (!q.From.IsZero() && !q.From.Before(q.To)) || len(q.ProviderID) > 512 || len(q.Model) > 512 {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid statistics range or filter"})
		return
	}
	if h.statisticsManager == nil {
		h.jsonResponse(w, http.StatusServiceUnavailable, Response{Success: false, Error: "Statistics unavailable"})
		return
	}
	summary, err := h.statisticsManager.Query(q, now)
	if err != nil {
		logging.Event(r.Context(), "statistics.read.failed", "status", "failed", "error_kind", logging.ErrorKind(err))
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to read statistics"})
		return
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: summary})
}
