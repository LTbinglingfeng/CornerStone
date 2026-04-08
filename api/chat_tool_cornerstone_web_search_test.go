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

func decodeCornerstoneWebSearchToolResult(t *testing.T, raw string) (chatToolResult, search.SearchResponse) {
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

func TestGetChatTools_CornerstoneWebSearchOnlyIncludedWhenEnabled(t *testing.T) {
	defaultTools := getChatTools()
	for _, tool := range defaultTools {
		if tool.Function.Name == cornerstoneWebSearchToolName {
			t.Fatalf("%s unexpectedly registered without configuration", cornerstoneWebSearchToolName)
		}
	}

	enabledTools := getChatTools(chatToolOptions{CornerstoneWebSearchEnabled: true})
	found := false
	for _, tool := range enabledTools {
		if tool.Function.Name == cornerstoneWebSearchToolName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s tool not registered when enabled", cornerstoneWebSearchToolName)
	}

	disabledByToggleTools := getChatTools(chatToolOptions{
		CornerstoneWebSearchEnabled: true,
		ToolToggles: map[string]bool{
			cornerstoneWebSearchToolName: false,
		},
	})
	found = false
	for _, tool := range disabledByToggleTools {
		if tool.Function.Name == cornerstoneWebSearchToolName {
			found = true
			break
		}
	}
	if found {
		t.Fatalf("%s should be hidden when disabled by toggle: %#v", cornerstoneWebSearchToolName, disabledByToggleTools)
	}

	disabledByLegacyToggleTools := getChatTools(chatToolOptions{
		CornerstoneWebSearchEnabled: true,
		ToolToggles: map[string]bool{
			legacyWebSearchToolName: false,
		},
	})
	for _, tool := range disabledByLegacyToggleTools {
		if tool.Function.Name == cornerstoneWebSearchToolName {
			t.Fatalf("%s should also be hidden by legacy toggle key: %#v", cornerstoneWebSearchToolName, disabledByLegacyToggleTools)
		}
	}
}

func TestIsCornerstoneWebSearchConfigured_RequiresProviderSpecificSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	if isCornerstoneWebSearchConfigured(cfg) {
		t.Fatal("expected web search to be disabled without an active provider")
	}

	cfg.CornerstoneWebSearch.ActiveProviderID = "tavily"
	cfg.CornerstoneWebSearch.Providers = map[string]config.WebSearchProvider{
		"tavily": {},
	}
	if isCornerstoneWebSearchConfigured(cfg) {
		t.Fatal("expected tavily to require an API key")
	}

	cfg.CornerstoneWebSearch.Providers["tavily"] = config.WebSearchProvider{APIKey: "secret-key"}
	if !isCornerstoneWebSearchConfigured(cfg) {
		t.Fatal("expected tavily to be enabled once its API key is configured")
	}

	cfg.CornerstoneWebSearch.ActiveProviderID = "searxng"
	cfg.CornerstoneWebSearch.Providers["searxng"] = config.WebSearchProvider{}
	if isCornerstoneWebSearchConfigured(cfg) {
		t.Fatal("expected searxng to require an API host")
	}

	cfg.CornerstoneWebSearch.Providers["searxng"] = config.WebSearchProvider{APIHost: "https://search.example.com"}
	if !isCornerstoneWebSearchConfigured(cfg) {
		t.Fatal("expected searxng to be enabled once its API host is configured")
	}
}

func TestChatTool_CornerstoneWebSearch_Integration(t *testing.T) {
	var gotReq map[string]interface{}
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path=%s want /search", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"hello","results":[{"title":"t1","url":"https://a.com","content":"c1"}]}`))
	}))
	defer srv.Close()

	cm := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	cfg := cm.Get()
	cfg.CornerstoneWebSearch.ActiveProviderID = "tavily"
	cfg.CornerstoneWebSearch.MaxResults = 3
	cfg.CornerstoneWebSearch.TimeoutSeconds = 5
	cfg.CornerstoneWebSearch.Providers = map[string]config.WebSearchProvider{
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

	executor := newChatToolExecutor()
	executor.configManager = cm
	executor.cornerstoneWebSearch = orch

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      cornerstoneWebSearchToolName,
			Arguments: `{"query":"hello"}`,
		},
	}, chatToolContext{})

	result, payload := decodeCornerstoneWebSearchToolResult(t, raw)
	if !result.OK {
		t.Fatalf("tool ok=false error=%q", result.Error)
	}
	if result.Tool != cornerstoneWebSearchToolName {
		t.Fatalf("result.tool=%q want %q", result.Tool, cornerstoneWebSearchToolName)
	}
	if payload.Query != "hello" {
		t.Fatalf("payload.query=%q want hello", payload.Query)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("len(payload.results)=%d want 1", len(payload.Results))
	}
	if gotAuth != "Bearer test_key" {
		t.Fatalf("Authorization=%q want %q", gotAuth, "Bearer test_key")
	}
	if gotReq["query"] != "hello" {
		t.Fatalf("req.query=%v want hello", gotReq["query"])
	}
}

func TestChatTool_CornerstoneWebSearch_LegacyToolNameAlias(t *testing.T) {
	executor := newChatToolExecutor()
	executor.configManager = config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	executor.cornerstoneWebSearch = search.NewOrchestrator(search.NewRegistry(), nil)

	result := executor.ExecuteResult(context.Background(), client.ToolCall{
		ID:   "call_legacy",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      legacyWebSearchToolName,
			Arguments: `{"query":"hello"}`,
		},
	}, chatToolContext{})

	if result.Tool != cornerstoneWebSearchToolName {
		t.Fatalf("result.Tool=%q want %q", result.Tool, cornerstoneWebSearchToolName)
	}
}
