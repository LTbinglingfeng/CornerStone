package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

const defaultTavilyAPIHost = "https://api.tavily.com"

type Tavily struct {
	httpClient *http.Client
}

func NewTavily(httpClient *http.Client) *Tavily {
	return &Tavily{httpClient: httpClient}
}

func (p *Tavily) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDTavily,
		Name:               "Tavily",
		RequiresAPIKey:     true,
		RequiresAPIHost:    false,
		SupportsExclude:    true,
		SupportsTimeFilter: true,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

type tavilySearchRequest struct {
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	TimeRange      string   `json:"time_range,omitempty"`
}

type tavilySearchResponse struct {
	Query   string `json:"query"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (p *Tavily) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDTavily, Message: "api_key is required"}
	}

	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		apiHost = defaultTavilyAPIHost
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDTavily, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	reqBody := tavilySearchRequest{
		Query:      strings.TrimSpace(query),
		MaxResults: providerFetchResults(cfg),
	}
	if len(cfg.ExcludeDomains) > 0 {
		reqBody.ExcludeDomains = cfg.ExcludeDomains
	}
	if cfg.SearchWithTime {
		// Best-effort recent filter matching other providers' past-week behavior.
		reqBody.TimeRange = "week"
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	var respBody tavilySearchResponse
	resp, _, errDo := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, headers, reqBody, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDTavily, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDTavily, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDTavily,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("upstream returned http %d", resp.StatusCode),
		}
	}

	out := &search.SearchResponse{
		Query:   strings.TrimSpace(respBody.Query),
		Results: nil,
	}
	if out.Query == "" {
		out.Query = strings.TrimSpace(query)
	}
	for _, item := range respBody.Results {
		out.Results = append(out.Results, search.SearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Content: strings.TrimSpace(item.Content),
		})
	}
	return out, nil
}
