package api

import (
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
)

// SessionRequest 会话请求
type SessionRequest struct {
	Title      string `json:"title,omitempty"`
	PromptID   string `json:"prompt_id,omitempty"`
	PromptName string `json:"prompt_name,omitempty"`
}

type SessionMessageUpdateRequest struct {
	Index   int    `json:"index"`
	Content string `json:"content"`
}

type SessionMessageDeleteRequest struct {
	Index int `json:"index"`
}

type SessionRedPacketOpenRequest struct {
	PacketKey    string `json:"packet_key"`
	ReceiverName string `json:"receiver_name,omitempty"`
	SenderName   string `json:"sender_name,omitempty"`
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		sessions := h.chatManager.ListSessions()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: sessions})

	case "POST":
		var req SessionRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		// 如果提供了 promptID 但没有提供 promptName，尝试从 promptManager 获取
		promptID := req.PromptID
		promptName := req.PromptName
		if promptID != "" && promptName == "" {
			if prompt, ok := h.promptManager.Get(promptID); ok {
				promptName = prompt.Name
			}
		}

		title := req.Title
		if title == "" {
			if promptName != "" {
				title = promptName
			} else {
				title = "New Chat"
			}
		}

		sessionID := generateID()
		session, err := h.chatManager.CreateSession(sessionID, title, promptID, promptName)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: session})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleSessionByID 处理单个会话请求
func (h *Handler) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/management/sessions/")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	parts := strings.Split(raw, "/")
	sessionID := parts[0]
	if sessionID == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Session ID required"})
		return
	}

	if len(parts) >= 2 && parts[1] == "messages" {
		h.handleSessionMessages(w, r, sessionID, parts[2:])
		return
	}

	switch r.Method {
	case "GET":
		session, ok := h.chatManager.GetSession(sessionID)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: session})

	case "PUT":
		var req SessionRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if err := h.chatManager.UpdateSessionTitle(sessionID, req.Title); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session updated"})

	case "DELETE":
		if err := h.chatManager.DeleteSession(sessionID); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Session deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string, parts []string) {
	if len(parts) == 0 {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Message action required"})
		return
	}

	action := parts[0]
	switch action {
	case "update":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.UpdateMessageContentByIndex(sessionID, req.Index, req.Content); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "recall":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageDeleteRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.RecallMessageByIndex(sessionID, req.Index); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrPermission) {
				h.jsonResponse(w, http.StatusForbidden, Response{Success: false, Error: "Only user messages can be recalled"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "delete":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}
		var req SessionMessageDeleteRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}
		if err := h.chatManager.DeleteMessageByIndex(sessionID, req.Index); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid message index"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	case "red-packet-open":
		if r.Method != http.MethodPost {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}

		var req SessionRedPacketOpenRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if err := h.chatManager.AddRedPacketReceivedBanner(sessionID, req.PacketKey, req.ReceiverName, req.SenderName); err != nil {
			if errors.Is(err, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid session ID"})
				return
			}
			if errors.Is(err, os.ErrInvalid) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid red packet key"})
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Session not found"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		updated, _ := h.chatManager.GetSession(sessionID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})
		return

	default:
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Unknown message action"})
		return
	}
}

// generateID 生成唯一ID
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

