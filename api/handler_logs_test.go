package api

import (
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Runs in a child process because logging.Init redirects the global logger.
func TestLogsEndpoints(t *testing.T) {
	if os.Getenv("CORNERSTONE_LOGS_ENDPOINT_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLogsEndpoints$", "-test.v")
		cmd.Env = append(os.Environ(), "CORNERSTONE_LOGS_ENDPOINT_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		return
	}

	logPath := filepath.Join(t.TempDir(), "cornerstone.log")
	file, err := logging.Init(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	logging.Infof("hello from test: user=alice")
	logging.Errorf("something broke: err=boom")

	h := &Handler{authManager: storage.NewAuthManager(t.TempDir()), tokenStore: newAuthTokenStore()}
	if _, err := h.authManager.Setup("owner", "long-test-password"); err != nil {
		t.Fatal(err)
	}
	token, err := h.tokenStore.create("owner", "owner")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.registerProtectedRoute(mux, logsPath, h.handleLogs)
	h.registerProtectedRoute(mux, logsDownloadPath, h.handleLogsDownload)
	do := func(target string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	if w := do(logsPath, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated logs access: %d", w.Code)
	}
	if w := do(logsDownloadPath, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated download: %d", w.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Entries []logging.LogEntry `json:"entries"`
			Cursor  int64              `json:"cursor"`
			File    string             `json:"file"`
		} `json:"data"`
	}
	w := do(logsPath+"?level=ERROR", true)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("logs: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || len(body.Data.Entries) != 1 || body.Data.File != logPath {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	entry := body.Data.Entries[0]
	if entry.Level != "ERROR" || entry.Message != "something broke" || len(entry.Fields) != 1 || entry.Fields[0].Value != "boom" {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	// Successful polls must not write request events into the file being viewed.
	cursor := body.Data.Cursor
	for i := 0; i < 3; i++ {
		do(logsPath+"?after="+strconv.FormatInt(cursor, 10), true)
	}
	w = do(logsPath+"?after="+strconv.FormatInt(cursor, 10), true)
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Entries) != 0 {
		t.Fatalf("poll requests were logged: %+v", body.Data.Entries)
	}

	for _, bad := range []string{"?level=TRACE", "?before=1&after=1", "?after=-1", "?limit=x"} {
		if w := do(logsPath+bad, true); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d", bad, w.Code)
		}
	}

	w = do(logsDownloadPath, true)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Body.String(), "hello from test") {
		t.Fatalf("download: %d %+v", w.Code, w.Header())
	}
}
