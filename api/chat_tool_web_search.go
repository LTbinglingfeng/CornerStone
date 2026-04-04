package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/internal/search"
	"fmt"
	"strings"
	"time"
)

type chatToolWebSearchArgs struct {
	Query string `json:"query"`
}

func (e *chatToolExecutor) handleWebSearch(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	if e.webSearch == nil {
		return chatToolResult{OK: false, Data: nil, Error: "web search not configured"}
	}
	if e.configManager == nil {
		return chatToolResult{OK: false, Data: nil, Error: "config manager not configured"}
	}

	var args chatToolWebSearchArgs
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return chatToolResult{OK: false, Data: nil, Error: "query is required"}
	}

	cfg := e.configManager.Get()
	settings := cfg.WebSearch
	providerID := strings.TrimSpace(settings.ActiveProviderID)
	if providerID == "" {
		return chatToolResult{OK: false, Data: nil, Error: "web search provider not configured"}
	}
	providerSettings, ok := settings.Providers[providerID]
	if !ok {
		return chatToolResult{OK: false, Data: nil, Error: "web search provider settings not found"}
	}

	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = search.DefaultTimeout
	}
	ctxSearch, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if e.emitEvent != nil {
		e.emitEvent(map[string]interface{}{
			"type":          "web_search_started",
			"provider_id":   providerID,
			"query":         query,
			"max_results":   settings.MaxResults,
			"fetch_results": settings.FetchResults,
		})
	}

	startedAt := time.Now()
	resp, errSearch := e.webSearch.Search(
		ctxSearch,
		providerID,
		search.ProviderConfig{
			APIKey:            strings.TrimSpace(providerSettings.APIKey),
			APIHost:           strings.TrimSpace(providerSettings.APIHost),
			BasicAuthUsername: strings.TrimSpace(providerSettings.BasicAuthUsername),
			BasicAuthPassword: strings.TrimSpace(providerSettings.BasicAuthPassword),
		},
		query,
		search.SearchConfig{
			MaxResults:     settings.MaxResults,
			FetchResults:   settings.FetchResults,
			ExcludeDomains: settings.ExcludeDomains,
			SearchWithTime: settings.SearchWithTime,
		},
	)

	duration := time.Since(startedAt)
	if e.emitEvent != nil {
		payload := map[string]interface{}{
			"type":        "web_search_completed",
			"provider_id": providerID,
			"query":       query,
			"duration_ms": duration.Milliseconds(),
		}
		if errSearch != nil {
			payload["error"] = search.PublicMessage(errSearch)
		} else if resp != nil {
			payload["results"] = len(resp.Results)
		} else {
			payload["results"] = 0
		}
		e.emitEvent(payload)
	}

	if errSearch != nil {
		return chatToolResult{
			OK:    false,
			Data:  nil,
			Error: fmt.Sprintf("web_search failed: %s", search.PublicMessage(errSearch)),
		}
	}

	if resp == nil {
		resp = &search.SearchResponse{
			Query:   query,
			Results: nil,
		}
	}
	return chatToolResult{
		OK:   true,
		Data: resp,
	}
}
