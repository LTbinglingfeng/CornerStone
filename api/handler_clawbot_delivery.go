package api

import (
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

func (s *ClawBotService) deliveryWorkerStopped() bool {
	s.mu.RLock()
	done := s.workerDone
	s.mu.RUnlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}
func deliveryActions(job storage.ClawBotDeliveryJob) []string {
	switch job.Status {
	case "uncertain", "blocked":
		// Only reachable as a projection while the worker is stopped; the worker
		// resolves these automatically on start.
		return []string{"retry", "cancel"}
	case "queued", "ready", "uploading":
		return []string{"cancel"}
	case "cancelled":
		if !job.HistoryCommitted {
			return []string{"retry"}
		}
		return []string{}
	default:
		return []string{}
	}
}
func projectStoppedDelivery(job *storage.ClawBotDeliveryJob) {
	switch job.Status {
	case "uploading":
		job.Status = "ready"
		job.Phase = "upload"
	case "sending":
		job.Status = "uncertain"
		job.Phase = "send"
	case "generating":
		job.Status = "uncertain"
		job.Phase = "generation"
	case "executing":
		job.Status = "uncertain"
		job.Phase = "command"
	case "preparing":
		job.Status = "queued"
	case "committing":
		job.Status = "blocked"
		job.Phase = "history"
	}
}

type clawBotPublicDelivery struct {
	ID             string                         `json:"id"`
	AccountID      string                         `json:"account_id"`
	Revision       uint64                         `json:"revision"`
	Kind           string                         `json:"kind"`
	UserID         string                         `json:"user_id"`
	SessionID      string                         `json:"session_id,omitempty"`
	HistoryPending bool                           `json:"history_pending"`
	Status         string                         `json:"status"`
	Phase          string                         `json:"phase,omitempty"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
	LastError      string                         `json:"last_error,omitempty"`
	NextAttemptAt  *time.Time                     `json:"next_attempt_at,omitempty"`
	Chunks         []storage.ClawBotDeliveryChunk `json:"chunks"`
	CurrentChunkID string                         `json:"current_chunk_id,omitempty"`
	AllowedActions []string                       `json:"allowed_actions"`
}

type clawBotDeliveryAction struct {
	ID          string `json:"id"`
	AccountID   string `json:"account_id"`
	Revision    uint64 `json:"revision"`
	Action      string `json:"action"`
	OperationID string `json:"operation_id"`
}

func (s *ClawBotService) deliveryList() []clawBotPublicDelivery {
	stopped := s.deliveryWorkerStopped()
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	result := []clawBotPublicDelivery{}
	if s.state.Delivery == nil {
		return result
	}
	for _, job := range s.state.Delivery.Jobs {
		if stopped {
			projectStoppedDelivery(&job)
		}
		row := clawBotPublicDelivery{ID: job.ID, AccountID: s.stateAccount, Revision: job.Revision, Kind: job.Kind, UserID: job.UserID, SessionID: job.SessionID, Status: job.Status, Phase: job.Phase, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, LastError: job.LastError, Chunks: append([]storage.ClawBotDeliveryChunk{}, job.Chunks...), AllowedActions: deliveryActions(job)}
		// Do not leak CDN download credentials, keys or private blob locations.
		for i := range row.Chunks {
			if media := row.Chunks[i].Media; media != nil {
				copy := *media
				copy.UploadStatus = "pending"
				if media.Uploaded != nil {
					copy.UploadStatus = "uploaded"
				}
				copy.Uploaded = nil
				copy.BlobID = ""
				copy.SourcePath = ""
				copy.HistoryPath = ""
				row.Chunks[i].Media = &copy
			}
		}
		row.HistoryPending = job.CancelRequested && !job.HistoryCommitted
		if !job.NextAttemptAt.IsZero() {
			retryAt := job.NextAttemptAt
			row.NextAttemptAt = &retryAt
		}
		if job.NextChunk < len(job.Chunks) {
			row.CurrentChunkID = job.Chunks[job.NextChunk].ClientID
		}
		result = append(result, row)
	}
	return result
}

func (s *ClawBotService) actOnDelivery(cfg config.ClawBotConfig, req clawBotDeliveryAction) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	cfg = s.handler.configManager.GetClawBotConfig()
	if req.AccountID != clawBotAccount(cfg) {
		return fmt.Errorf("delivery account changed")
	}
	if err := storage.ValidateID(req.OperationID); err != nil {
		return fmt.Errorf("invalid delivery operation ID")
	}
	stopped := s.deliveryWorkerStopped()
	fingerprintBytes, _ := json.Marshal(req)
	fingerprint := string(fingerprintBytes)
	err := s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		if prior, ok := d.Actions[req.OperationID]; ok {
			if prior != fingerprint {
				return fmt.Errorf("operation ID reused with different intent")
			}
			return nil
		}
		j := deliveryJob(d, req.ID)
		if j == nil {
			return errClawBotJobUnavailable
		}
		if j.Revision != req.Revision {
			return fmt.Errorf("delivery changed since it was reviewed")
		}
		if stopped {
			projectStoppedDelivery(j)
		}
		if !slices.Contains(deliveryActions(*j), req.Action) {
			return fmt.Errorf("action is not valid in the current state")
		}
		switch req.Action {
		case "cancel":
			j.CancelRequested = true
			j.Status = "committing"
			j.Phase = "history"
		case "retry":
			switch j.Phase {
			case "send", "upload":
				j.Status = "ready"
				if j.NextChunk < len(j.Chunks) {
					j.Chunks[j.NextChunk].Status = "pending"
				}
			case "history":
				j.Status = "committing"
			default:
				j.Status = "queued"
			}
		}
		j.LastError = ""
		j.NextAttemptAt = time.Time{}
		j.ReadyAt = time.Now()
		j.Attempts = 0
		j.Revision++
		j.UpdatedAt = time.Now()
		d.Actions[req.OperationID] = fingerprint
		return nil
	})
	if err != nil {
		return err
	}
	if stopped {
		// Pure history/cancellation reconciliation is safe even while the channel is
		// disabled, and is performed only after the old worker has fully exited.
		if job, ok := s.snapshotDelivery(req.ID); ok && job.Status == "committing" {
			s.commitDelivery(cfg, job)
		}
	}
	s.applyConfigLocked(s.handler.configManager.GetClawBotConfig())
	return nil
}

func (h *Handler) handleClawBotDeliveries(w http.ResponseWriter, r *http.Request) {
	if h.clawBotService == nil || h.clawBotService.stateManager == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Durable ClawBot delivery is unavailable"})
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var req clawBotDeliveryAction
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := storage.ValidateID(req.ID); err != nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid delivery ID"})
			return
		}
		if err := h.clawBotService.actOnDelivery(h.configManager.GetClawBotConfig(), req); err != nil {
			h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Delivery action failed; refresh the queue and check its current state"})
			return
		}
	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: struct {
		Jobs []clawBotPublicDelivery `json:"jobs"`
	}{h.clawBotService.deliveryList()}})
}
