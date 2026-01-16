package api

import (
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	memorySessionMaxIdle     = 30 * time.Minute // 会话最大空闲时间
	memorySessionCleanupTick = 5 * time.Minute  // 清理检查间隔
)

type MemoryUpsertRequest struct {
	Subject  string   `json:"subject,omitempty"`
	Category string   `json:"category,omitempty"`
	Content  string   `json:"content,omitempty"`
	Strength *float64 `json:"strength,omitempty"`
}

type SetMemoryProviderRequest struct {
	ProviderID string `json:"provider_id"`
}

type SetMemoryEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) getOrCreateMemorySession(promptID, sessionID string) *storage.MemorySession {
	if h.memoryManager == nil || h.configManager == nil {
		return nil
	}
	if sessionID == "" {
		return nil
	}

	cfg := h.configManager.Get()
	if !cfg.MemoryEnabled {
		return nil
	}

	// 启动清理 goroutine（只执行一次）
	h.cleanupOnce.Do(func() {
		go h.cleanupMemorySessions()
	})

	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	if h.memorySessions == nil {
		h.memorySessions = make(map[string]*storage.MemorySession)
	}

	if session, ok := h.memorySessions[sessionID]; ok {
		return session
	}
	if promptID == "" {
		return nil
	}

	session := storage.NewMemorySession(promptID, sessionID, h.memoryManager, h.memoryExtractor)
	h.memorySessions[sessionID] = session
	return session
}

// cleanupMemorySessions 定期清理空闲的记忆会话
func (h *Handler) cleanupMemorySessions() {
	ticker := time.NewTicker(memorySessionCleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-h.cleanupDone:
			return
		case <-ticker.C:
			h.doCleanupMemorySessions()
		}
	}
}

func (h *Handler) doCleanupMemorySessions() {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	now := time.Now()
	for sessionID, session := range h.memorySessions {
		if now.Sub(session.LastAccess()) > memorySessionMaxIdle {
			delete(h.memorySessions, sessionID)
			logging.Infof("清理空闲记忆会话: %s", sessionID)
		}
	}
}

func (h *Handler) handleMemory(w http.ResponseWriter, r *http.Request) {
	if h.memoryManager == nil {
		h.jsonResponse(w, http.StatusNotImplemented, Response{Success: false, Error: "Memory manager not configured"})
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/api/memory/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Prompt ID required"})
		return
	}

	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 1:
		promptID := parts[0]
		switch r.Method {
		case http.MethodGet:
			h.handleGetMemories(w, r, promptID)
		case http.MethodPost:
			h.handleAddMemory(w, r, promptID)
		default:
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		}

	case 2:
		promptID := parts[0]
		memoryID := parts[1]
		switch r.Method {
		case http.MethodPut:
			h.handleUpdateMemory(w, r, promptID, memoryID)
		case http.MethodDelete:
			h.handleDeleteMemory(w, r, promptID, memoryID)
		default:
			h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		}

	default:
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Not found"})
	}
}

func (h *Handler) handleGetMemories(w http.ResponseWriter, r *http.Request, promptID string) {
	responses := h.memoryManager.GetAllResponses(promptID)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: responses})
}

func (h *Handler) handleAddMemory(w http.ResponseWriter, r *http.Request, promptID string) {
	var req MemoryUpsertRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Category) == "" || strings.TrimSpace(req.Content) == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "subject/category/content required"})
		return
	}
	if req.Subject != storage.SubjectUser && req.Subject != storage.SubjectSelf {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "invalid subject"})
		return
	}

	memory := storage.Memory{
		Subject:   strings.TrimSpace(req.Subject),
		Category:  strings.TrimSpace(req.Category),
		Content:   strings.TrimSpace(req.Content),
		SeenCount: 1,
	}

	if req.Strength == nil {
		memory.Strength = storage.DefaultStrengthForCategory(memory.Category)
	} else {
		memory.Strength = *req.Strength
	}

	if errAdd := h.memoryManager.Add(promptID, memory); errAdd != nil {
		if errors.Is(errAdd, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errAdd.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleUpdateMemory(w http.ResponseWriter, r *http.Request, promptID, memoryID string) {
	var req MemoryUpsertRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	patch := storage.MemoryPatch{
		ID: memoryID,
	}
	subject := strings.TrimSpace(req.Subject)
	if subject != "" {
		if subject != storage.SubjectUser && subject != storage.SubjectSelf {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "invalid subject"})
			return
		}
		patch.Subject = &subject
	}
	category := strings.TrimSpace(req.Category)
	if category != "" {
		patch.Category = &category
	}
	content := strings.TrimSpace(req.Content)
	if content != "" {
		patch.Content = &content
	}
	if req.Strength != nil {
		patch.Strength = req.Strength
	}

	if errUpdate := h.memoryManager.Patch(promptID, patch); errUpdate != nil {
		if errors.Is(errUpdate, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID or memory ID"})
			return
		}
		if errors.Is(errUpdate, os.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Memory not found"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleDeleteMemory(w http.ResponseWriter, r *http.Request, promptID, memoryID string) {
	if errDelete := h.memoryManager.Delete(promptID, memoryID); errDelete != nil {
		if errors.Is(errDelete, storage.ErrInvalidID) {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid prompt ID or memory ID"})
			return
		}
		if errors.Is(errDelete, os.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Memory not found"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errDelete.Error()})
		return
	}

	h.handleGetMemories(w, r, promptID)
}

func (h *Handler) handleSetMemoryProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req SetMemoryProviderRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	providerID := strings.TrimSpace(req.ProviderID)
	if providerID != "" {
		provider := h.configManager.GetProvider(providerID)
		if provider == nil {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Provider not found"})
			return
		}
		if provider.Type == config.ProviderTypeGeminiImage {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider is not chat-capable"})
			return
		}
	}

	cfg := h.configManager.Get()
	cfg.MemoryProviderID = providerID
	cfg.MemoryProvider = nil
	if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_provider_id": providerID}})
}

func (h *Handler) handleSetMemoryEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	var req SetMemoryEnabledRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}

	cfg := h.configManager.Get()
	cfg.MemoryEnabled = req.Enabled
	if errUpdate := h.configManager.Update(cfg); errUpdate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errUpdate.Error()})
		return
	}

	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{"memory_enabled": req.Enabled}})
}

