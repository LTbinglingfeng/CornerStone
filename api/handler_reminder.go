package api

import (
	"cornerstone/storage"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type ReminderUpdateRequest struct {
	Title          *string `json:"title,omitempty"`
	ReminderPrompt *string `json:"reminder_prompt,omitempty"`
	DueAt          *string `json:"due_at,omitempty"`
}

type reminderResponse struct {
	ID             string                  `json:"id"`
	Channel        storage.ReminderChannel `json:"channel"`
	SessionID      string                  `json:"session_id"`
	SessionTitle   string                  `json:"session_title,omitempty"`
	SessionExists  bool                    `json:"session_exists"`
	PromptID       string                  `json:"prompt_id"`
	PromptName     string                  `json:"prompt_name"`
	PromptExists   bool                    `json:"prompt_exists"`
	ClawBotUserID  string                  `json:"clawbot_user_id,omitempty"`
	Title          string                  `json:"title"`
	ReminderPrompt string                  `json:"reminder_prompt"`
	DueAt          time.Time               `json:"due_at"`
	Status         storage.ReminderStatus  `json:"status"`
	Attempts       int                     `json:"attempts"`
	LastError      string                  `json:"last_error,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	FiredAt        *time.Time              `json:"fired_at,omitempty"`
}

func parseReminderDueAt(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func (h *Handler) handleReminders(w http.ResponseWriter, r *http.Request) {
	if h.reminderService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Reminder service not configured"})
		return
	}
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	reminders := h.reminderService.List()
	result := make([]reminderResponse, 0, len(reminders))
	for _, reminder := range reminders {
		result = append(result, h.buildReminderResponse(reminder))
	}
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: result})
}

func (h *Handler) handleReminderByID(w http.ResponseWriter, r *http.Request) {
	if h.reminderService == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Reminder service not configured"})
		return
	}

	raw := strings.TrimPrefix(r.URL.Path, "/api/settings/reminders/")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Reminder ID required"})
		return
	}

	parts := strings.Split(raw, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Reminder ID required"})
		return
	}

	if len(parts) == 2 && parts[1] == "cancel" {
		h.handleReminderCancel(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		reminder, ok := h.reminderService.Get(id)
		if !ok || reminder == nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Reminder not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: h.buildReminderResponse(*reminder)})

	case http.MethodPut:
		var req ReminderUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		var patch storage.ReminderPatch
		if req.Title != nil {
			title := strings.TrimSpace(*req.Title)
			if title == "" {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "title is required"})
				return
			}
			patch.Title = &title
		}
		if req.ReminderPrompt != nil {
			reminderPrompt := strings.TrimSpace(*req.ReminderPrompt)
			if reminderPrompt == "" {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "reminder_prompt is required"})
				return
			}
			patch.ReminderPrompt = &reminderPrompt
		}
		if req.DueAt != nil {
			dueAt, errParse := parseReminderDueAt(*req.DueAt)
			if errParse != nil {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "invalid due_at"})
				return
			}
			patch.DueAt = &dueAt
		}
		if patch.Title == nil && patch.ReminderPrompt == nil && patch.DueAt == nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "no reminder fields to update"})
			return
		}

		updated, errUpdate := h.reminderService.UpdatePending(id, patch)
		if errUpdate != nil {
			switch {
			case errors.Is(errUpdate, os.ErrNotExist):
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Reminder not found"})
			case errors.Is(errUpdate, storage.ErrReminderImmutable):
				h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Only pending reminders can be edited"})
			default:
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: errUpdate.Error()})
			}
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: h.buildReminderResponse(*updated)})

	case http.MethodDelete:
		if errDelete := h.reminderService.Delete(id); errDelete != nil {
			if errors.Is(errDelete, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Reminder not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errDelete.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Reminder deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleReminderCancel(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	cancelled, errCancel := h.reminderService.CancelPending(id)
	if errCancel != nil {
		switch {
		case errors.Is(errCancel, os.ErrNotExist):
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Reminder not found"})
		case errors.Is(errCancel, storage.ErrReminderImmutable):
			h.jsonResponse(w, http.StatusConflict, Response{Success: false, Error: "Only pending reminders can be cancelled"})
		default:
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: errCancel.Error()})
		}
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: h.buildReminderResponse(*cancelled)})
}

func (h *Handler) buildReminderResponse(reminder storage.Reminder) reminderResponse {
	promptName := strings.TrimSpace(reminder.PromptName)
	promptExists := false
	if h.promptManager != nil {
		if prompt, ok := h.promptManager.Get(reminder.PromptID); ok && prompt != nil {
			promptName = strings.TrimSpace(prompt.Name)
			promptExists = true
		}
	}
	if promptName == "" {
		promptName = reminder.PromptID
	}

	sessionTitle := ""
	sessionExists := false
	if h.chatManager != nil {
		if session, ok := h.chatManager.GetSession(reminder.SessionID); ok && session != nil {
			sessionTitle = strings.TrimSpace(session.Title)
			sessionExists = true
		}
	}

	return reminderResponse{
		ID:             reminder.ID,
		Channel:        reminder.Channel,
		SessionID:      reminder.SessionID,
		SessionTitle:   sessionTitle,
		SessionExists:  sessionExists,
		PromptID:       reminder.PromptID,
		PromptName:     promptName,
		PromptExists:   promptExists,
		ClawBotUserID:  reminder.ClawBotUserID,
		Title:          reminder.Title,
		ReminderPrompt: reminder.ReminderPrompt,
		DueAt:          reminder.DueAt,
		Status:         reminder.Status,
		Attempts:       reminder.Attempts,
		LastError:      reminder.LastError,
		CreatedAt:      reminder.CreatedAt,
		UpdatedAt:      reminder.UpdatedAt,
		FiredAt:        reminder.FiredAt,
	}
}
