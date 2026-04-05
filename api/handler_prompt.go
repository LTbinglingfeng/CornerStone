package api

import (
	"cornerstone/storage"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// PromptRequest 提示词创建请求
type PromptRequest struct {
	ID          string  `json:"id,omitempty"`
	Name        string  `json:"name"`
	Content     string  `json:"content"`
	Description *string `json:"description,omitempty"`
	FileName    *string `json:"file_name,omitempty"`
}

// PromptUpdateRequest 提示词更新请求
type PromptUpdateRequest struct {
	Name        *string `json:"name"`
	Content     *string `json:"content"`
	Description *string `json:"description"`
	FileName    *string `json:"file_name"`
}

func (h *Handler) handlePrompts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		prompts := h.listPromptsForManagement()
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompts})

	case "POST":
		var req PromptRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		if req.Name == "" || req.Content == "" {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Name and content are required"})
			return
		}

		id := req.ID
		if id == "" {
			id = generatePromptID()
		}

		prompt, err := h.promptManager.Create(id, req.Name, req.Content, stringValue(req.Description), stringValue(req.FileName))
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusCreated, Response{Success: true, Data: prompt})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// handlePromptByID 处理单个提示词请求
func (h *Handler) handlePromptByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/management/prompts/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		prompt, ok := h.getPromptForManagement(id)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompt})

	case "PUT":
		var req PromptUpdateRequest
		if !h.decodeJSON(w, r, &req) {
			return
		}

		existing, ok := h.getPromptForManagement(id)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		_, persisted := h.promptManager.Get(id)

		name := existing.Name
		content := existing.Content
		description := existing.Description
		fileName := existing.FileName

		if req.Name != nil {
			if strings.TrimSpace(*req.Name) == "" {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Name is required"})
				return
			}
			name = *req.Name
		}
		if req.Content != nil {
			if strings.TrimSpace(*req.Content) == "" {
				h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Content is required"})
				return
			}
			content = *req.Content
		}
		if req.Description != nil {
			description = *req.Description
		}
		if req.FileName != nil {
			fileName = *req.FileName
		}

		var (
			prompt *storage.Prompt
			err    error
		)
		if persisted {
			prompt, err = h.promptManager.Update(id, name, content, description, fileName)
		} else {
			prompt, err = h.promptManager.Create(id, name, content, description, fileName)
		}
		if err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: prompt})

	case "DELETE":
		if err := h.promptManager.Delete(id); err != nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Prompt deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// handlePromptSessions 处理获取指定 Prompt 的所有聊天记录请求
func (h *Handler) handlePromptSessions(w http.ResponseWriter, r *http.Request) {
	promptID := strings.TrimPrefix(r.URL.Path, "/management/prompts-sessions/")
	if promptID == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		// 先检查 prompt 是否存在；缺失 prompt.json 时，允许从历史会话恢复展示。
		if _, ok := h.getPromptForManagement(promptID); !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}

		sessions := h.chatManager.GetSessionsByPromptID(promptID)
		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: sessions})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

func (h *Handler) listPromptsForManagement() []storage.Prompt {
	promptMap := make(map[string]storage.Prompt)
	if h.promptManager != nil {
		for _, prompt := range h.promptManager.List() {
			promptMap[prompt.ID] = prompt
		}
	}

	if h.chatManager == nil {
		return mapPromptValues(promptMap)
	}

	for _, session := range h.chatManager.ListSessions() {
		promptID := strings.TrimSpace(session.PromptID)
		if promptID == "" {
			continue
		}
		if _, exists := promptMap[promptID]; exists {
			continue
		}

		recovered := recoveredPromptFromSession(promptID, session)
		if current, exists := promptMap[promptID]; !exists || recovered.UpdatedAt.After(current.UpdatedAt) {
			promptMap[promptID] = recovered
		}
	}

	return mapPromptValues(promptMap)
}

func (h *Handler) getPromptForManagement(id string) (*storage.Prompt, bool) {
	if h.promptManager != nil {
		if prompt, ok := h.promptManager.Get(id); ok {
			return prompt, true
		}
	}

	if h.chatManager != nil {
		sessions := h.chatManager.GetSessionsByPromptID(id)
		if len(sessions) > 0 {
			recovered := recoveredPromptFromSessions(id, sessions)
			return &recovered, true
		}
	}

	if h.memoryManager != nil && len(h.memoryManager.GetAll(id)) > 0 {
		now := time.Now()
		recovered := storage.Prompt{
			ID:        id,
			Name:      id,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return &recovered, true
	}

	return nil, false
}

func recoveredPromptFromSessions(promptID string, sessions []storage.ChatSession) storage.Prompt {
	recovered := recoveredPromptFromSession(promptID, sessions[0])
	for i := 1; i < len(sessions); i++ {
		candidate := recoveredPromptFromSession(promptID, sessions[i])
		if candidate.UpdatedAt.After(recovered.UpdatedAt) {
			recovered = candidate
		}
	}
	return recovered
}

func recoveredPromptFromSession(promptID string, session storage.ChatSession) storage.Prompt {
	name := strings.TrimSpace(session.PromptName)
	if name == "" {
		name = strings.TrimSpace(session.Title)
	}
	if name == "" {
		name = promptID
	}

	createdAt := session.CreatedAt
	updatedAt := session.UpdatedAt
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
		updatedAt = createdAt
	}

	return storage.Prompt{
		ID:        promptID,
		Name:      name,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

func mapPromptValues(promptMap map[string]storage.Prompt) []storage.Prompt {
	prompts := make([]storage.Prompt, 0, len(promptMap))
	for _, prompt := range promptMap {
		prompts = append(prompts, prompt)
	}
	return prompts
}

func (h *Handler) handlePromptAvatar(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/management/prompts-avatar/")
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	switch r.Method {
	case "GET":
		// 获取头像
		avatarPath, err := h.promptManager.GetAvatarPath(id)
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
		// 上传头像
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

		savedFilename, err := h.promptManager.SaveAvatar(id, filename, file)
		if err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: savedFilename})

	case "DELETE":
		// 删除头像
		prompt, ok := h.promptManager.Get(id)
		if !ok {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Prompt not found"})
			return
		}

		if prompt.Avatar == "" {
			h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "No avatar to delete"})
			return
		}

		// 设置头像为空
		if err := h.promptManager.SetAvatar(id, ""); err != nil {
			h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: err.Error()})
			return
		}

		h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: "Avatar deleted"})

	default:
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
	}
}

// generatePromptID 生成Prompt专用ID (3位数字+3位英文字母)
func generatePromptID() string {
	// 生成3位数字
	numBytes := make([]byte, 2)
	rand.Read(numBytes)
	num := int(numBytes[0])<<8 | int(numBytes[1])
	num = num % 1000 // 0-999

	// 生成3位字母
	letters := "abcdefghijklmnopqrstuvwxyz"
	letterBytes := make([]byte, 3)
	rand.Read(letterBytes)

	result := fmt.Sprintf("%03d", num)
	for i := 0; i < 3; i++ {
		result += string(letters[int(letterBytes[i])%len(letters)])
	}

	return result
}
