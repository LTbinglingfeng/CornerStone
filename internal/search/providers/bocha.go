package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

// Cherry Studio uses: POST {apiHost}/v1/web-search
// Body: { query, count, exclude, freshness, summary, page }
const defaultBochaAPIHost = "https://api.bochaai.com"

type Bocha struct {
	httpClient *http.Client
}

func NewBocha(httpClient *http.Client) *Bocha {
	return &Bocha{httpClient: httpClient}
}

func (p *Bocha) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDBocha,
		Name:               "Bocha",
		RequiresAPIKey:     true,
		RequiresAPIHost:    false,
		SupportsExclude:    true,
		SupportsTimeFilter: true,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

type bochaSearchRequest struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Exclude   string `json:"exclude,omitempty"`
	Freshness string `json:"freshness,omitempty"`
	Summary   bool   `json:"summary,omitempty"`
	Page      int    `json:"page,omitempty"`
}

type bochaSearchResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		QueryContext struct {
			OriginalQuery string `json:"originalQuery"`
		} `json:"queryContext"`
		WebPages struct {
			Value []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
				Summary string `json:"summary"`
			} `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

func (p *Bocha) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDBocha, Message: "api_key is required"}
	}

	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		apiHost = defaultBochaAPIHost
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/v1/web-search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDBocha, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	reqBody := bochaSearchRequest{
		Query:   strings.TrimSpace(query),
		Count:   providerFetchResults(cfg),
		Summary: true,
		Page:    1,
	}
	if len(cfg.ExcludeDomains) > 0 {
		reqBody.Exclude = strings.Join(cfg.ExcludeDomains, ",")
	}
	if cfg.SearchWithTime {
		// Best-effort: prefer recent results within roughly a week.
		reqBody.Freshness = "oneWeek"
	} else {
		reqBody.Freshness = "noLimit"
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	var respBody bochaSearchResponse
	resp, _, errDo := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, headers, reqBody, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDBocha, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDBocha, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDBocha,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("upstream returned http %d", resp.StatusCode),
		}
	}
	if respBody.Code != 200 {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDBocha, Message: fmt.Sprintf("provider error: %s", strings.TrimSpace(respBody.Msg))}
	}

	out := &search.SearchResponse{
		Query:   strings.TrimSpace(respBody.Data.QueryContext.OriginalQuery),
		Results: nil,
	}
	if out.Query == "" {
		out.Query = strings.TrimSpace(query)
	}
	for _, item := range respBody.Data.WebPages.Value {
		content := strings.TrimSpace(item.Summary)
		if content == "" {
			content = strings.TrimSpace(item.Snippet)
		}
		out.Results = append(out.Results, search.SearchResult{
			Title:   strings.TrimSpace(item.Name),
			URL:     strings.TrimSpace(item.URL),
			Content: content,
		})
	}
	return out, nil
}
