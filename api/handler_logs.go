package api

import (
	"cornerstone/logging"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	logsPath         = "/api/logs"
	logsDownloadPath = "/api/logs/download"
)

// quietRequestLogPaths are polled by the log viewer; logging every successful
// poll would flood the very file being viewed. Failures are still logged.
var quietRequestLogPaths = map[string]bool{logsPath: true, statisticsPath: true}

type logsResponse struct {
	logging.LogPage
	File string `json:"file"`
}

var validLogLevels = map[string]bool{"INFO": true, "WARN": true, "ERROR": true, "FATAL": true}

// handleLogs serves bounded, parsed pages of the application log.
// `before` pages backwards from a line offset, `after` follows appended lines.
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	query := r.URL.Query()
	q := logging.LogQuery{Search: query.Get("q")}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid limit"})
			return
		}
		q.Limit = limit
	}
	if len([]rune(q.Search)) > 200 {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Search query too long"})
		return
	}
	for _, part := range strings.Split(query.Get("level"), ",") {
		level := strings.ToUpper(strings.TrimSpace(part))
		if level == "" {
			continue
		}
		if !validLogLevels[level] {
			h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid level"})
			return
		}
		if q.Levels == nil {
			q.Levels = make(map[string]bool)
		}
		q.Levels[level] = true
	}
	for _, prefix := range query["exclude_event"] {
		if prefix = strings.TrimSpace(prefix); prefix != "" && len(q.ExcludeEventPrefixes) < 8 {
			q.ExcludeEventPrefixes = append(q.ExcludeEventPrefixes, prefix)
		}
	}
	parseOffset := func(name string) (int64, bool, bool) {
		raw := strings.TrimSpace(query.Get(name))
		if raw == "" {
			return 0, false, true
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return 0, true, false
		}
		return value, true, true
	}
	before, hasBefore, okBefore := parseOffset("before")
	after, hasAfter, okAfter := parseOffset("after")
	if !okBefore || !okAfter || (hasBefore && hasAfter) {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid cursor"})
		return
	}

	path := logging.Path()
	var (
		page logging.LogPage
		err  error
	)
	switch {
	case hasAfter:
		page, err = logging.ReadLogAfter(path, after, q)
	case hasBefore:
		page, err = logging.ReadLogBefore(path, before, q)
	default:
		page, err = logging.ReadLogBefore(path, -1, q)
	}
	if err != nil {
		logging.Event(r.Context(), "logs.read.failed", "status", "failed", "error_kind", logging.ErrorKind(err))
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to read log file"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: logsResponse{LogPage: page, File: path}})
}

// handleLogsDownload streams the log file as it was when the request began.
func (h *Handler) handleLogsDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}
	path := logging.Path()
	if path == "" {
		h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Log file not found"})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			h.jsonResponse(w, http.StatusNotFound, Response{Success: false, Error: "Log file not found"})
			return
		}
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to read log file"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		h.jsonResponse(w, http.StatusInternalServerError, Response{Success: false, Error: "Failed to read log file"})
		return
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := fmt.Sprintf("%s-%s.log", base, h.now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, f, info.Size())
}
