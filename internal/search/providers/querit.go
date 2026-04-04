package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

const defaultQueritAPIHost = "https://api.querit.ai"

type Querit struct {
	httpClient *http.Client
}

func NewQuerit(httpClient *http.Client) *Querit {
	return &Querit{httpClient: httpClient}
}

func (p *Querit) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDQuerit,
		Name:               "Querit",
		RequiresAPIKey:     true,
		RequiresAPIHost:    false,
		SupportsExclude:    true,
		SupportsTimeFilter: true,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

type queritSearchRequest struct {
	Query   string `json:"query"`
	Count   int    `json:"count"`
	Filters *struct {
		Sites *struct {
			Exclude []string `json:"exclude,omitempty"`
		} `json:"sites,omitempty"`
		TimeRange *struct {
			Date string `json:"date,omitempty"`
		} `json:"timeRange,omitempty"`
	} `json:"filters,omitempty"`
}

type queritSearchResponse struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
	QueryCtx  struct {
		Query string `json:"query"`
	} `json:"query_context"`
	Results struct {
		Result []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"result"`
	} `json:"results"`
}

func (p *Querit) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDQuerit, Message: "api_key is required"}
	}

	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		apiHost = defaultQueritAPIHost
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/v1/search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDQuerit, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	reqBody := queritSearchRequest{
		Query: strings.TrimSpace(query),
		Count: cfg.MaxResults,
	}
	if reqBody.Count <= 0 {
		reqBody.Count = 1
	}

	var filters *struct {
		Sites *struct {
			Exclude []string `json:"exclude,omitempty"`
		} `json:"sites,omitempty"`
		TimeRange *struct {
			Date string `json:"date,omitempty"`
		} `json:"timeRange,omitempty"`
	}

	if len(cfg.ExcludeDomains) > 0 || cfg.SearchWithTime {
		filters = &struct {
			Sites *struct {
				Exclude []string `json:"exclude,omitempty"`
			} `json:"sites,omitempty"`
			TimeRange *struct {
				Date string `json:"date,omitempty"`
			} `json:"timeRange,omitempty"`
		}{}
	}

	if len(cfg.ExcludeDomains) > 0 {
		filters.Sites = &struct {
			Exclude []string `json:"exclude,omitempty"`
		}{
			Exclude: cfg.ExcludeDomains,
		}
	}
	if cfg.SearchWithTime {
		// Best-effort: d7 (past 7 days).
		filters.TimeRange = &struct {
			Date string `json:"date,omitempty"`
		}{Date: "d7"}
	}
	if filters != nil {
		reqBody.Filters = filters
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	var respBody queritSearchResponse
	resp, raw, errDo := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, headers, reqBody, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDQuerit, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDQuerit, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDQuerit,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))),
		}
	}
	if respBody.ErrorCode != 200 {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDQuerit, Message: fmt.Sprintf("provider error: %s", strings.TrimSpace(respBody.ErrorMsg))}
	}

	out := &search.SearchResponse{
		Query:   strings.TrimSpace(respBody.QueryCtx.Query),
		Results: nil,
	}
	if out.Query == "" {
		out.Query = strings.TrimSpace(query)
	}
	for _, item := range respBody.Results.Result {
		out.Results = append(out.Results, search.SearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Content: strings.TrimSpace(item.Snippet),
		})
	}
	return out, nil
}
