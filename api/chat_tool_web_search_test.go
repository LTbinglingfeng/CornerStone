package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/internal/search"
	"cornerstone/internal/search/providers"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func decodeWebSearchToolResult(t *testing.T, raw string) (chatToolResult, search.SearchResponse) {
	t.Helper()

	var result chatToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal chatToolResult failed: %v", err)
	}

	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("Marshal result.Data failed: %v", err)
	}

	var payload search.SearchResponse
	if len(dataBytes) > 0 && string(dataBytes) != "null" {
		if err := json.Unmarshal(dataBytes, &payload); err != nil {
			t.Fatalf("Unmarshal SearchResponse failed: %v", err)
		}
	}
	return result, payload
}

func TestChatTool_WebSearch_Integration(t *testing.T) {
	var gotReq map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path=%s want /search", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"hello","results":[{"title":"t1","url":"https://a.com","content":"c1"}]}`))
	}))
	defer srv.Close()

	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := cm.Get()
	cfg.WebSearch.ActiveProviderID = "tavily"
	cfg.WebSearch.MaxResults = 3
	cfg.WebSearch.TimeoutSeconds = 5
	cfg.WebSearch.Providers = map[string]config.WebSearchProvider{
		"tavily": {
			APIKey:  "test_key",
			APIHost: srv.URL,
		},
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("config update: %v", err)
	}

	reg := search.NewRegistry()
	if err := providers.RegisterAll(reg); err != nil {
		t.Fatalf("register providers: %v", err)
	}
	orch := search.NewOrchestrator(reg, srv.Client(), search.WithTimeout(2*time.Second))

	executor := newChatToolExecutor(nil, nil)
	executor.configManager = cm
	executor.webSearch = orch

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "web_search",
			Arguments: `{"query":"hello"}`,
		},
	}, chatToolContext{})

	result, payload := decodeWebSearchToolResult(t, raw)
	if !result.OK {
		t.Fatalf("tool ok=false error=%q", result.Error)
	}
	if payload.Query != "hello" {
		t.Fatalf("payload.query=%q want hello", payload.Query)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("len(payload.results)=%d want 1", len(payload.Results))
	}
	if gotReq["api_key"] != "test_key" {
		t.Fatalf("req.api_key=%v want test_key", gotReq["api_key"])
	}
	if gotReq["query"] != "hello" {
		t.Fatalf("req.query=%v want hello", gotReq["query"])
	}
}
