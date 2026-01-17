package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type createMomentRequest struct {
	PromptID    string `json:"prompt_id"`
	PromptName  string `json:"prompt_name"`
	Content     string `json:"content"`
	ImagePrompt string `json:"image_prompt"`
}

type momentLikeRequest struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

type momentCommentRequest struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Content  string `json:"content"`
	ReplyTo  string `json:"reply_to,omitempty"`
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, errAtoi := strconv.Atoi(raw)
	if errAtoi != nil {
		return fallback
	}
	return value
}

func (h *Handler) handleMoments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := parseIntQuery(r, "limit", 20)
		offset := parseIntQuery(r, "offset", 0)
		moments := h.momentManager.List(limit, offset)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: moments})

	case http.MethodPost:
		var req createMomentRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		req.PromptID = strings.TrimSpace(req.PromptID)
		req.PromptName = strings.TrimSpace(req.PromptName)
		req.Content = strings.TrimSpace(req.Content)
		req.ImagePrompt = strings.TrimSpace(req.ImagePrompt)

		if req.PromptID == "" || req.PromptName == "" || req.Content == "" || req.ImagePrompt == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid request"})
			return
		}

		now := time.Now()
		moment := storage.Moment{
			ID:          uuid.NewString(),
			PromptID:    req.PromptID,
			PromptName:  req.PromptName,
			Content:     req.Content,
			ImagePrompt: req.ImagePrompt,
			Status:      storage.MomentStatusPending,
			CreatedAt:   now,
			UpdatedAt:   now,
			Likes:       []storage.Like{},
			Comments:    []storage.Comment{},
		}

		created, errCreate := h.momentManager.Create(moment)
		if errCreate != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errCreate.Error()})
			return
		}

		h.momentGenerator.StartGeneration(created.ID)
		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: created})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleMomentByID(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/moments/")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Moment ID required"})
		return
	}

	parts := strings.Split(raw, "/")
	momentID := strings.TrimSpace(parts[0])
	if momentID == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Moment ID required"})
		return
	}

	if len(parts) >= 2 {
		switch parts[1] {
		case "like":
			h.handleMomentLike(w, r, momentID)
			return
		case "comments":
			h.handleMomentComments(w, r, momentID, parts[2:])
			return
		default:
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Unknown action"})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		moment, ok := h.momentManager.Get(momentID)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: moment})

	case http.MethodDelete:
		h.momentGenerator.CancelGeneration(momentID)
		if errDelete := h.momentManager.Delete(momentID); errDelete != nil {
			if errors.Is(errDelete, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
				return
			}
			if errors.Is(errDelete, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid moment ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errDelete.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Moment deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleMomentLike(w http.ResponseWriter, r *http.Request, momentID string) {
	switch r.Method {
	case http.MethodPost:
		var req momentLikeRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		req.UserType = strings.TrimSpace(req.UserType)
		req.UserID = strings.TrimSpace(req.UserID)
		req.UserName = strings.TrimSpace(req.UserName)
		if req.UserType == "" || req.UserID == "" || req.UserName == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid request"})
			return
		}

		like := storage.Like{
			ID:        uuid.NewString(),
			UserType:  req.UserType,
			UserID:    req.UserID,
			UserName:  req.UserName,
			CreatedAt: time.Now(),
		}

		if _, errLike := h.momentManager.AddLike(momentID, like); errLike != nil {
			if errors.Is(errLike, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
				return
			}
			if errors.Is(errLike, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid moment ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errLike.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: true})

	case http.MethodDelete:
		userType := strings.TrimSpace(r.URL.Query().Get("user_type"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if userType == "" || userID == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "user_type and user_id are required"})
			return
		}

		if _, errUnlike := h.momentManager.RemoveLike(momentID, userType, userID); errUnlike != nil {
			if errors.Is(errUnlike, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
				return
			}
			if errors.Is(errUnlike, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid moment ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUnlike.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: true})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleMomentComments(w http.ResponseWriter, r *http.Request, momentID string, parts []string) {
	if len(parts) >= 1 {
		commentID := strings.TrimSpace(parts[0])
		if commentID == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Comment ID required"})
			return
		}

		if r.Method != http.MethodDelete {
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
			return
		}

		if _, errDelete := h.momentManager.RemoveComment(momentID, commentID); errDelete != nil {
			if errors.Is(errDelete, os.ErrNotExist) {
				h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
				return
			}
			if errors.Is(errDelete, storage.ErrInvalidID) {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid moment ID"})
				return
			}
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errDelete.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: true})
		return
	}

	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req momentCommentRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	req.UserType = strings.TrimSpace(req.UserType)
	req.UserID = strings.TrimSpace(req.UserID)
	req.UserName = strings.TrimSpace(req.UserName)
	req.Content = strings.TrimSpace(req.Content)
	req.ReplyTo = strings.TrimSpace(req.ReplyTo)

	if req.UserType == "" || req.UserID == "" || req.UserName == "" || req.Content == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid request"})
		return
	}

	comment := storage.Comment{
		ID:        uuid.NewString(),
		UserType:  req.UserType,
		UserID:    req.UserID,
		UserName:  req.UserName,
		Content:   req.Content,
		ReplyTo:   req.ReplyTo,
		CreatedAt: time.Now(),
	}

	if _, errComment := h.momentManager.AddComment(momentID, comment); errComment != nil {
		if errors.Is(errComment, os.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Moment not found"})
			return
		}
		if errors.Is(errComment, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid moment ID"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errComment.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: comment})
}

func (h *Handler) handleMomentsConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.momentManager.GetConfig()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: cfg})

	case http.MethodPut:
		var cfg storage.MomentsConfig
		if !h.decodeJSON(w, r, &cfg) {
			return
		}
		updated, errUpdate := h.momentManager.UpdateConfig(cfg)
		if errUpdate != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: updated})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) handleMomentsBackgroundUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBodyBytes)
	if errParse := r.ParseMultipartForm(10 << 20); errParse != nil {
		var maxErr *http.MaxBytesError
		if errors.As(errParse, &maxErr) {
			h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
			return
		}
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
		return
	}

	file, header, errForm := r.FormFile("background")
	if errForm != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get background file"})
		return
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			logging.Errorf("关闭背景图文件失败: %v", errClose)
		}
	}()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		ext = ".jpg"
	}

	filename := "bg_" + uuid.NewString() + ext
	dstPath := h.momentManager.GetBackgroundPath(filename)
	dst, errCreate := os.Create(dstPath)
	if errCreate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to save background"})
		return
	}
	dstClosed := false
	defer func() {
		if dstClosed {
			return
		}
		if errClose := dst.Close(); errClose != nil {
			logging.Errorf("关闭背景图目标文件失败: %v", errClose)
		}
	}()

	if _, errCopy := io.Copy(dst, file); errCopy != nil {
		dstClosed = true
		if errClose := dst.Close(); errClose != nil {
			logging.Errorf("关闭背景图目标文件失败: %v", errClose)
		}
		_ = os.Remove(dstPath)
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to save background"})
		return
	}

	relPath := "moments/backgrounds/" + filename
	if _, errSet := h.momentManager.SetBackgroundImage(relPath); errSet != nil {
		logging.Errorf("moments background config save failed: %v", errSet)
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]string{"path": relPath}})
}
