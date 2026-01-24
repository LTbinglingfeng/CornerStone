package api

import (
	"cornerstone/storage"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	ttsAudioDirName = "tts_audio"
)

func (h *Handler) resolveTTSAudioPath(relPath string) (string, error) {
	cleaned := strings.ReplaceAll(relPath, "\\", "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid audio path")
	}

	cleaned = strings.TrimPrefix(cleaned, "./")
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid audio path")
	}

	fileName := cleaned
	if strings.HasPrefix(cleaned, ttsAudioDirName+"/") {
		fileName = strings.TrimPrefix(cleaned, ttsAudioDirName+"/")
	}
	if errValidateFileName := storage.ValidateFileName(fileName); errValidateFileName != nil {
		return "", errValidateFileName
	}

	return filepath.Join(h.ttsAudioDir, fileName), nil
}

func (h *Handler) handleTTSAudioByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/management/tts-audio/")
	if name == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Audio name required"})
		return
	}

	relPath := path.Join(ttsAudioDirName, name)
	audioPath, errResolve := h.resolveTTSAudioPath(relPath)
	if errResolve != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Audio not found"})
		return
	}
	if _, errStat := os.Stat(audioPath); errStat != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Audio not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(audioPath))
	contentType := "application/octet-stream"
	switch ext {
	case ".mp3":
		contentType = "audio/mpeg"
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, audioPath)
}
