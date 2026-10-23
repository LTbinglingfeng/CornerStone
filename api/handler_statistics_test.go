package api

import (
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatisticsEndpoint(t *testing.T) {
	dir := t.TempDir()
	manager, err := storage.NewStatisticsManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, record := range []client.CallRecord{
		{CompletedAt: now.Add(-2 * time.Hour), ProviderID: "p1", Model: "m1", Success: true, UsageReported: true, InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CacheReadTokens: 40},
		{CompletedAt: now.Add(-time.Second), ProviderID: "p2", Model: "m2", Success: false},
	} {
		if err := manager.RecordCall(record); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{authManager: storage.NewAuthManager(t.TempDir()), tokenStore: newAuthTokenStore()}
	h.SetStatisticsManager(manager)
	if _, err := h.authManager.Setup("owner", "long-test-password"); err != nil {
		t.Fatal(err)
	}
	token, err := h.tokenStore.create("owner", "owner")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	do := func(method, query string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, statisticsPath+query, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := do(http.MethodGet, "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated statistics: %d", w.Code)
	}
	decode := func(query string) storage.StatisticsSummary {
		t.Helper()
		w := do(http.MethodGet, query, true)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("statistics: %d %s", w.Code, w.Body.String())
		}
		var response struct {
			Success bool                      `json:"success"`
			Data    storage.StatisticsSummary `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !response.Success {
			t.Fatalf("response: %s %v", w.Body.String(), err)
		}
		return response.Data
	}
	s := decode("")
	if s.Totals.Requests != 2 || s.Totals.TotalTokens != 120 || s.Realtime.RPM != 1 || s.Realtime.TPM != 0 || s.Realtime.UnknownUsageRequests != 1 || len(s.Groups) != 2 || !s.DataComplete {
		t.Fatalf("summary: %+v", s)
	}
	s = decode("?provider_id=p1&model=m1")
	if s.Totals.Requests != 1 || s.Realtime.RPM != 0 || len(s.Groups) != 1 || *s.Totals.CacheHitRate != 0.4 {
		t.Fatalf("filtered: %+v", s)
	}
	s = decode("?from=" + now.Add(-3*time.Hour).Format(time.RFC3339Nano) + "&to=" + now.Add(-time.Hour).Format(time.RFC3339Nano))
	if s.Totals.Requests != 1 || s.Realtime.RPM != 1 {
		t.Fatalf("history incorrectly limited realtime: %+v", s)
	}
	s = decode("?model=unknown")
	if s.Totals.Requests != 0 || s.Totals.CacheHitRate != nil || s.Totals.UsageCoverage != nil || len(s.Groups) != 0 {
		t.Fatalf("empty result: %+v", s)
	}
	for _, query := range []string{
		"?from=invalid", "?to=", "?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z",
		"?from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z", "?model=a&model=b", "?unexpected=x",
		"?provider_id=" + strings.Repeat("a", 513),
	} {
		if w := do(http.MethodGet, query, true); w.Code != http.StatusBadRequest {
			t.Errorf("invalid query %s: %d", query, w.Code)
		}
	}
	if w := do(http.MethodPost, "", true); w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method: %d", w.Code)
	}
	h.SetStatisticsManager(nil)
	if w := do(http.MethodGet, "", true); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing manager: %d", w.Code)
	}
	h.SetStatisticsManager(manager)
	if err := os.WriteFile(filepath.Join(dir, now.Format("2006-01-02")+".jsonl"), []byte("BROKEN_SECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := do(http.MethodGet, "", true); w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "BROKEN_SECRET") || strings.Contains(w.Body.String(), dir) {
		t.Fatalf("corruption response: %d %s", w.Code, w.Body.String())
	}
}
