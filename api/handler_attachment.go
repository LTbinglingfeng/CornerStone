package api

import (
	"cornerstone/storage"
	"mime"
	"net/http"
	"path/filepath"
)

func (h *Handler) attachmentStore() storage.AttachmentStore {
	return storage.AttachmentStore{Dir: filepath.Join(filepath.Dir(h.cachePhotoDir), "attachments")}
}
func (h *Handler) handleAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	f, err := h.attachmentStore().Open(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
