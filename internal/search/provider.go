package search

import (
	"context"
	"net/http"
)

type Provider interface {
	Info() ProviderInfo
	Search(ctx context.Context, query string, cfg SearchConfig, providerCfg ProviderConfig) (*SearchResponse, error)
}

type ProviderFactory func(httpClient *http.Client) Provider
