package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

// UserInfoRequest 用户信息请求
type UserInfoRequest struct {
	Username    string `json:"username,omitempty"`
	Description string `json:"description,omitempty"`
}

func (h *Handler) handleUser(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		userInfo := h.userManager.Get()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: userInfo})

	case "PUT", "POST":
		var req UserInfoRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		userInfo, err := h.userManager.Update(req.Username, req.Description)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		if req.Username != "" && h.authManager != nil {
			if err := h.authManager.UpdateUsername(req.Username); err != nil && !errors.Is(err, storage.ErrAuthNotSetup) {
				logging.Errorf("sync auth username failed: %v", err)
			}
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: userInfo})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handleUserAvatar 处理用户头像请求
func (h *Handler) handleUserAvatar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		avatarPath, err := h.userManager.GetAvatarPath()
		if err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Avatar not found"})
			return
		}

		// 根据文件扩展名设置Content-Type
		ext := strings.ToLower(filepath.Ext(avatarPath))
		contentType := "application/octet-stream"
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".svg":
			contentType = "image/svg+xml"
		}

		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, avatarPath)

	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBodyBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
				return
			}
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
			return
		}

		file, header, err := r.FormFile("avatar")
		if err != nil {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get avatar file"})
			return
		}
		defer file.Close()

		// 获取文件扩展名
		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".png"
		}

		// 使用 avatar + 扩展名作为文件名
		filename := "avatar" + ext

		savedFilename, err := h.userManager.SaveAvatar(filename, file)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: savedFilename})

	case "DELETE":
		if err := h.userManager.DeleteAvatar(); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Avatar deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

