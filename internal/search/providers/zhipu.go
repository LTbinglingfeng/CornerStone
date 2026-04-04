package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

type Zhipu struct {
	httpClient *http.Client
}

func NewZhipu(httpClient *http.Client) *Zhipu {
	return &Zhipu{httpClient: httpClient}
}

func (p *Zhipu) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDZhipu,
		Name:               "Zhipu",
		RequiresAPIKey:     true,
		RequiresAPIHost:    true, // Zhipu uses a full endpoint URL in Cherry Studio.
		SupportsExclude:    false,
		SupportsTimeFilter: false,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

type zhipuWebSearchRequest struct {
	SearchQuery  string `json:"search_query"`
	SearchEngine string `json:"search_engine,omitempty"`
	SearchIntent bool   `json:"search_intent,omitempty"`
}

type zhipuWebSearchResponse struct {
	SearchResult []struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Link    string `json:"link"`
	} `json:"search_result"`
}

func (p *Zhipu) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDZhipu, Message: "api_key is required"}
	}

	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDZhipu, Message: "api_host is required"}
	}

	reqBody := zhipuWebSearchRequest{
		SearchQuery:  strings.TrimSpace(query),
		SearchEngine: "search_std",
		SearchIntent: false,
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	var respBody zhipuWebSearchResponse
	resp, raw, errDo := doJSON(ctx, p.httpClient, http.MethodPost, apiHost, headers, reqBody, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDZhipu, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDZhipu, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDZhipu,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))),
		}
	}

	out := &search.SearchResponse{
		Query:   strings.TrimSpace(query),
		Results: nil,
	}
	for _, item := range respBody.SearchResult {
		out.Results = append(out.Results, search.SearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.Link),
			Content: strings.TrimSpace(item.Content),
		})
	}
	return out, nil
}
