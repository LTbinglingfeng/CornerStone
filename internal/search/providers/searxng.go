package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type SearxNG struct {
	httpClient *http.Client
}

func NewSearxNG(httpClient *http.Client) *SearxNG {
	return &SearxNG{httpClient: httpClient}
}

func (p *SearxNG) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDSearxNG,
		Name:               "SearxNG",
		RequiresAPIKey:     false,
		RequiresAPIHost:    true,
		SupportsExclude:    false,
		SupportsTimeFilter: false,
		SupportsBasicAuth:  true,
		SupportsMaxResults: false,
	}
}

type searxngSearchResponse struct {
	Query   string `json:"query"`
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (p *SearxNG) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDSearxNG, Message: "api_host is required"}
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDSearxNG, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	parsed, errParse := url.Parse(endpoint)
	if errParse != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDSearxNG, Message: "invalid api_host", Cause: errParse}
	}
	q := parsed.Query()
	q.Set("format", "json")
	q.Set("q", strings.TrimSpace(query))
	q.Set("language", "auto")
	parsed.RawQuery = q.Encode()

	headers := map[string]string{}
	if auth := basicAuthHeader(strings.TrimSpace(providerCfg.BasicAuthUsername), strings.TrimSpace(providerCfg.BasicAuthPassword)); auth != "" {
		headers["Authorization"] = auth
	}

	var respBody searxngSearchResponse
	resp, _, errDo := doJSON(ctx, p.httpClient, http.MethodGet, parsed.String(), headers, nil, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDSearxNG, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDSearxNG, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDSearxNG,
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
	_ = cfg // searxng does not support max_results in the simple JSON API; orchestrator will trim.
	return out, nil
}
