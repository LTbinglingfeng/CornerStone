package search

import (
	"context"
	"cornerstone/logging"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultMaxResults = 5
	MaxResultsLimit   = 50
	DefaultTimeout    = 20 * time.Second
)

type Orchestrator struct {
	registry   *Registry
	httpClient *http.Client
	timeout    time.Duration
}

type OrchestratorOption func(*Orchestrator)

func WithTimeout(timeout time.Duration) OrchestratorOption {
	return func(o *Orchestrator) {
		if timeout > 0 {
			o.timeout = timeout
		}
	}
}

func NewOrchestrator(registry *Registry, httpClient *http.Client, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		registry:   registry,
		httpClient: httpClient,
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

func (o *Orchestrator) ProviderInfos() []ProviderInfo {
	if o == nil || o.registry == nil {
		return nil
	}
	return o.registry.Infos(o.httpClient)
}

func (o *Orchestrator) Search(ctx context.Context, providerID string, providerCfg ProviderConfig, query string, cfg SearchConfig) (*SearchResponse, error) {
	if o == nil || o.registry == nil {
		return nil, fmt.Errorf("search orchestrator not configured")
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, &Error{Kind: ErrKindInvalidRequest, ProviderID: strings.TrimSpace(providerID), Message: "query is required"}
	}

	normalizedCfg := normalizeSearchConfig(cfg)
	startedAt := time.Now()

	timeout := o.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// Honor existing deadlines (e.g. per-request setting) by only applying a timeout when needed.
	ctxSearch := ctx
	cancel := func() {}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > timeout {
		ctxSearch, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	provider, errProvider := o.registry.Create(providerID, o.httpClient)
	if errProvider != nil {
		return nil, errProvider
	}

	logging.Infof(
		"web_search start: provider=%s query=%q max_results=%d fetch_results=%d exclude_domains=%d with_time=%v",
		strings.TrimSpace(provider.Info().ID),
		logging.Truncate(trimmedQuery, 160),
		normalizedCfg.MaxResults,
		normalizedCfg.FetchResults,
		len(normalizedCfg.ExcludeDomains),
		normalizedCfg.SearchWithTime,
	)

	resp, errSearch := provider.Search(ctxSearch, trimmedQuery, normalizedCfg, providerCfg)
	if errSearch != nil {
		duration := time.Since(startedAt)
		if errors.Is(errSearch, context.DeadlineExceeded) || errors.Is(ctxSearch.Err(), context.DeadlineExceeded) {
			logging.Warnf("web_search timeout: provider=%s duration=%s err=%v", strings.TrimSpace(provider.Info().ID), duration, errSearch)
			return nil, &Error{
				Kind:       ErrKindTimeout,
				ProviderID: strings.TrimSpace(provider.Info().ID),
				Message:    "search timed out",
				Cause:      errSearch,
			}
		}
		logging.Errorf("web_search failed: provider=%s duration=%s err=%v", strings.TrimSpace(provider.Info().ID), duration, errSearch)
		return nil, errSearch
	}

	if resp == nil {
		resp = &SearchResponse{Query: trimmedQuery, Results: nil}
	}
	if strings.TrimSpace(resp.Query) == "" {
		resp.Query = trimmedQuery
	}

	resp.Results = normalizeResults(resp.Results)
	if len(normalizedCfg.ExcludeDomains) > 0 && len(resp.Results) > 0 {
		resp.Results = filterExcludedResults(resp.Results, normalizedCfg.ExcludeDomains)
	}
	if normalizedCfg.MaxResults > 0 && len(resp.Results) > normalizedCfg.MaxResults {
		resp.Results = resp.Results[:normalizedCfg.MaxResults]
	}

	logging.Infof(
		"web_search done: provider=%s duration=%s results=%d",
		strings.TrimSpace(provider.Info().ID),
		time.Since(startedAt),
		len(resp.Results),
	)
	return resp, nil
}

func normalizeSearchConfig(cfg SearchConfig) SearchConfig {
	normalized := SearchConfig{
		MaxResults:     cfg.MaxResults,
		FetchResults:   cfg.FetchResults,
		ExcludeDomains: nil,
		SearchWithTime: cfg.SearchWithTime,
	}
	if normalized.MaxResults <= 0 {
		normalized.MaxResults = DefaultMaxResults
	}
	if normalized.MaxResults > MaxResultsLimit {
		normalized.MaxResults = MaxResultsLimit
	}
	if normalized.FetchResults <= 0 {
		normalized.FetchResults = normalized.MaxResults
	}
	if normalized.FetchResults > MaxResultsLimit {
		normalized.FetchResults = MaxResultsLimit
	}
	if normalized.FetchResults < normalized.MaxResults {
		normalized.FetchResults = normalized.MaxResults
	}

	excludeSet := make(map[string]struct{})
	for _, raw := range cfg.ExcludeDomains {
		domain := normalizeDomain(raw)
		if domain == "" {
			continue
		}
		if _, exists := excludeSet[domain]; exists {
			continue
		}
		excludeSet[domain] = struct{}{}
		normalized.ExcludeDomains = append(normalized.ExcludeDomains, domain)
	}
	return normalized
}

func normalizeResults(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return nil
	}
	normalized := make([]SearchResult, 0, len(results))
	for _, item := range results {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.Content)
		link := strings.TrimSpace(item.URL)
		if title == "" && content == "" && link == "" {
			continue
		}
		title = truncateRunes(title, 200)
		content = truncateRunes(content, 2000)
		normalized = append(normalized, SearchResult{
			Title:   title,
			URL:     link,
			Content: content,
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func filterExcludedResults(results []SearchResult, excludeDomains []string) []SearchResult {
	if len(results) == 0 || len(excludeDomains) == 0 {
		return results
	}
	filtered := make([]SearchResult, 0, len(results))
	for _, item := range results {
		if isURLExcluded(item.URL, excludeDomains) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func isURLExcluded(rawURL string, excludeDomains []string) bool {
	link := strings.TrimSpace(rawURL)
	if link == "" || len(excludeDomains) == 0 {
		return false
	}
	parsed, errParse := url.Parse(link)
	if errParse != nil || parsed == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		if h, _, errSplit := net.SplitHostPort(host); errSplit == nil && strings.TrimSpace(h) != "" {
			host = strings.TrimSpace(h)
		}
	}
	for _, domain := range excludeDomains {
		d := normalizeDomain(domain)
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func normalizeDomain(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, ".")
	if strings.Contains(trimmed, "://") {
		if u, err := url.Parse(trimmed); err == nil && u != nil {
			trimmed = strings.TrimSpace(u.Host)
		}
	} else if strings.Contains(trimmed, "/") {
		if u, err := url.Parse("https://" + trimmed); err == nil && u != nil {
			trimmed = strings.TrimSpace(u.Host)
		}
	}
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ":") {
		if host, _, errSplit := net.SplitHostPort(trimmed); errSplit == nil && strings.TrimSpace(host) != "" {
			trimmed = strings.TrimSpace(host)
		}
	}
	trimmed = strings.TrimPrefix(trimmed, ".")
	return strings.TrimSpace(trimmed)
}
