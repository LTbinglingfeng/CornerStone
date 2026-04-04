package providers

import (
	"context"
	"cornerstone/internal/search"
	"fmt"
	"net/http"
	"strings"
)

const defaultExaAPIHost = "https://api.exa.ai"

type Exa struct {
	httpClient *http.Client
}

func NewExa(httpClient *http.Client) *Exa {
	return &Exa{httpClient: httpClient}
}

func (p *Exa) Info() search.ProviderInfo {
	return search.ProviderInfo{
		ID:                 ProviderIDExa,
		Name:               "Exa",
		RequiresAPIKey:     true,
		RequiresAPIHost:    false,
		SupportsExclude:    false,
		SupportsTimeFilter: false,
		SupportsBasicAuth:  false,
		SupportsMaxResults: true,
	}
}

type exaSearchRequest struct {
	Query      string `json:"query"`
	NumResults int    `json:"numResults,omitempty"`
	Contents   struct {
		Text bool `json:"text"`
	} `json:"contents,omitempty"`
}

type exaSearchResponse struct {
	AutopromptString string `json:"autopromptString"`
	Results          []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Text  string `json:"text"`
	} `json:"results"`
}

func (p *Exa) Search(ctx context.Context, query string, cfg search.SearchConfig, providerCfg search.ProviderConfig) (*search.SearchResponse, error) {
	apiKey := strings.TrimSpace(providerCfg.APIKey)
	if apiKey == "" {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDExa, Message: "api_key is required"}
	}

	apiHost := strings.TrimSpace(providerCfg.APIHost)
	if apiHost == "" {
		apiHost = defaultExaAPIHost
	}

	endpoint, errEndpoint := resolveEndpoint(apiHost, "/search")
	if errEndpoint != nil {
		return nil, &search.Error{Kind: search.ErrKindProviderNotConfigured, ProviderID: ProviderIDExa, Message: errEndpoint.Error(), Cause: errEndpoint}
	}

	reqBody := exaSearchRequest{
		Query:      strings.TrimSpace(query),
		NumResults: providerFetchResults(cfg),
	}
	reqBody.Contents.Text = true

	headers := map[string]string{
		"x-api-key":     apiKey,
		"Authorization": "Bearer " + apiKey,
	}

	var respBody exaSearchResponse
	resp, _, errDo := doJSON(ctx, p.httpClient, http.MethodPost, endpoint, headers, reqBody, &respBody)
	if errDo != nil {
		return nil, &search.Error{Kind: search.ErrKindUpstream, ProviderID: ProviderIDExa, Message: "request failed", Cause: errDo}
	}
	if resp == nil {
		return nil, &search.Error{Kind: search.ErrKindBadResponse, ProviderID: ProviderIDExa, Message: "empty response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &search.Error{
			Kind:       search.ErrKindUpstream,
			ProviderID: ProviderIDExa,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("upstream returned http %d", resp.StatusCode),
		}
	}

	out := &search.SearchResponse{
		Query:   strings.TrimSpace(respBody.AutopromptString),
		Results: nil,
	}
	if out.Query == "" {
		out.Query = strings.TrimSpace(query)
	}
	for _, item := range respBody.Results {
		out.Results = append(out.Results, search.SearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Content: strings.TrimSpace(item.Text),
		})
	}
	return out, nil
}
