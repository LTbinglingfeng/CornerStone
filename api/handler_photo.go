package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxChatImageBodyBytes int64 = 11 << 20 // 10MB + overhead

	cachePhotoDirName = "cache_photo"
)

func (h *Handler) resolveCachePhotoPath(relPath string) (string, error) {
	cleaned := strings.ReplaceAll(relPath, "\\", "/")
	cleaned = path.Clean(cleaned)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid image path")
	}

	cleaned = strings.TrimPrefix(cleaned, "./")
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid image path")
	}

	fileName := cleaned
	if strings.HasPrefix(cleaned, cachePhotoDirName+"/") {
		fileName = strings.TrimPrefix(cleaned, cachePhotoDirName+"/")
	}
	if errValidateFileName := storage.ValidateFileName(fileName); errValidateFileName != nil {
		return "", errValidateFileName
	}

	return filepath.Join(h.cachePhotoDir, fileName), nil
}

// handleCachePhoto 处理聊天图片上传
func (h *Handler) handleCachePhoto(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatImageBodyBytes)
	if errParseForm := r.ParseMultipartForm(10 << 20); errParseForm != nil {
		var maxErr *http.MaxBytesError
		if errors.As(errParseForm, &maxErr) {
			h.jsonResponse(w, http.StatusRequestEntityTooLarge, Response{Success: false, Error: "Request body too large"})
			return
		}
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid multipart form"})
		return
	}

	file, header, errFormFile := r.FormFile("image")
	if errFormFile != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Failed to get image file"})
		return
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			logging.Warnf("close upload file error: %v", errClose)
		}
	}()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".png"
	}

	filename := generateID() + ext
	if errValidateFileName := storage.ValidateFileName(filename); errValidateFileName != nil {
		filename = generateID() + ".png"
	}
	if errValidateFileName := storage.ValidateFileName(filename); errValidateFileName != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid image filename"})
		return
	}

	savedPath := filepath.Join(h.cachePhotoDir, filename)
	output, errCreate := os.Create(savedPath)
	if errCreate != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errCreate.Error()})
		return
	}
	defer func() {
		if errClose := output.Close(); errClose != nil {
			logging.Warnf("close cached image error: %v", errClose)
		}
	}()

	if _, errCopy := io.Copy(output, file); errCopy != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: errCopy.Error()})
		return
	}

	relPath := path.Join(cachePhotoDirName, filename)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: relPath})
}

// handleCachePhotoByName 读取聊天图片
func (h *Handler) handleCachePhotoByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/management/cache-photo/")
	if name == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Image name required"})
		return
	}

	relPath := path.Join(cachePhotoDirName, name)
	imagePath, errResolve := h.resolveCachePhotoPath(relPath)
	if errResolve != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Image not found"})
		return
	}
	if _, errStat := os.Stat(imagePath); errStat != nil {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Image not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(imagePath))
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
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, imagePath)
}

