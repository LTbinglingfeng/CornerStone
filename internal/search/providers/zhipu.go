package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

const defaultZhipuAPIHost = "https://open.bigmodel.cn/api"

const (
	ZhipuSearchEngineStd      = "search_std"
	ZhipuSearchEnginePro      = "search_pro"
	ZhipuSearchEngineProSogou = "search_pro_sogou"
	ZhipuSearchEngineProQuark = "search_pro_quark"
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
		RequiresAPIHost:    false,
		SupportsExclude:    false,
		SupportsTimeFilter: true,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

func NormalizeZhipuSearchEngine(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ZhipuSearchEngineStd:
		return ZhipuSearchEngineStd
	case ZhipuSearchEnginePro:
		return ZhipuSearchEnginePro
	case ZhipuSearchEngineProSogou:
		return ZhipuSearchEngineProSogou
	case ZhipuSearchEngineProQuark:
		return ZhipuSearchEngineProQuark
	default:
		return ZhipuSearchEngineStd
	}
}

func IsValidZhipuSearchEngine(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == ZhipuSearchEngineStd ||
		normalized == ZhipuSearchEnginePro ||
		normalized == ZhipuSearchEngineProSogou ||
		normalized == ZhipuSearchEngineProQuark
}

type zhipuWebSearchRequest struct {
	SearchQuery         string `json:"search_query"`
	SearchEngine        string `json:"search_engine"`
	SearchIntent        bool   `json:"search_intent"`
	Count               int    `json:"count,omitempty"`
	SearchRecencyFilter string `json:"search_recency_filter,omitempty"`
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
		apiHost = defaultZhipuAPIHost
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/paas/v4/web_search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDZhipu, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	reqBody := zhipuWebSearchRequest{
		SearchQuery:  strings.TrimSpace(query),
		SearchEngine: NormalizeZhipuSearchEngine(providerCfg.SearchEngine),
		SearchIntent: false,
		Count:        providerFetchResults(cfg),
	}
	if cfg.SearchWithTime {
		reqBody.SearchRecencyFilter = "oneWeek"
	} else {
		reqBody.SearchRecencyFilter = "noLimit"
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	var respBody zhipuWebSearchResponse
	resp, _, errDo := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, headers, reqBody, &respBody)
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
			Message:    fmt.Sprintf("upstream returned http %d", resp.StatusCode),
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
