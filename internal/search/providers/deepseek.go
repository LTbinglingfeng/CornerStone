package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

const defaultDeepSeekAPIHost = "https://api.deepseek.com"

type DeepSeek struct{ httpClient *http.Client }

func NewDeepSeek(httpClient *http.Client) *DeepSeek { return &DeepSeek{httpClient: httpClient} }

func (p *DeepSeek) Info() search.ProviderInfo {
	return search.ProviderInfo{ID: ProviderIDDeepSeek, Name: "DeepSeek", RequiresAPIKey: true, SupportsMaxResults: true}
}

type deepSeekSearchRequest struct {
	Query       string `json:"query"`
	Count       int    `json:"count"`
	NeedContent bool   `json:"need_content"`
}

type deepSeekSearchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Summary string `json:"summary"`
		Snippet string `json:"snippet"`
		Content string `json:"content"`
	} `json:"results"`
}

func (p *DeepSeek) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" || !utf8.ValidString(query) || utf8.RuneCountInString(query) > 128 {
		return nil, &search.Error{Kind: search.ErrKindInvalidRequest, ProviderID: ProviderIDDeepSeek, Message: "query must contain 1–128 Unicode characters"}
	}
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDDeepSeek, Message: "api_key is required"}
	}
	apiHost := strings.TrimRight(strings.TrimSpace(providerCfg.APIHost), "/")
	if apiHost == "" {
		apiHost = defaultDeepSeekAPIHost
	}
	// Accept both the root host and the conventional /v1 base URL.
	relativePath := "/v1/tools/search"
	if strings.HasSuffix(apiHost, "/v1") {
		relativePath = "/tools/search"
	}
	endpoint, err := resolveEndpoint(apiHost, relativePath)
	if err != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDDeepSeek, Message: err.Error(), Cause: err}
	}
	count := min(providerFetchResults(cfg), 50)
	// The API's long summary is intended for model consumption; avoid fetching full pages.
	reqBody := deepSeekSearchRequest{Query: query, Count: count, NeedContent: false}
	resp, data, err := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, map[string]string{"Authorization": "Bearer " + apiKey}, reqBody, nil)
	if err != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDDeepSeek, Message: "request failed", Cause: err}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDDeepSeek, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDDeepSeek, StatusCode: resp.StatusCode, Message: fmt.Sprintf("upstream returned http %d", resp.StatusCode)}
	}
	var body deepSeekSearchResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDDeepSeek, Message: "invalid search response", Cause: err}
	}
	if body.Results == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDDeepSeek, Message: "missing results array"}
	}
	out := &search.SearchResponse{Query: query, Results: make([]search.SearchResult, 0, len(body.Results))}
	for _, item := range body.Results {
		content := strings.TrimSpace(item.Summary)
		if content == "" {
			content = strings.TrimSpace(item.Snippet)
		}
		if content == "" {
			content = strings.TrimSpace(item.Content)
		}
		out.Results = append(out.Results, search.SearchResult{Title: strings.TrimSpace(item.Title), URL: strings.TrimSpace(item.URL), Content: content})
	}
	return out, nil
}
