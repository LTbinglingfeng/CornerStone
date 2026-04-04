package search

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type programmableProvider struct {
	info    ProviderInfo
	delay   time.Duration
	resp    *SearchResponse
	err     error
	lastCfg SearchConfig
}

func (p *programmableProvider) Info() ProviderInfo {
	return p.info
}

func (p *programmableProvider) Search(ctx context.Context, query string, cfg SearchConfig, providerCfg ProviderConfig) (*SearchResponse, error) {
	p.lastCfg = cfg
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return p.resp, p.err
}

func TestOrchestrator_SearchProviderNotFound(t *testing.T) {
	reg := NewRegistry()
	o := NewOrchestrator(reg, http.DefaultClient, WithTimeout(50*time.Millisecond))

	_, err := o.Search(context.Background(), "missing", ProviderConfig{}, "hello", SearchConfig{MaxResults: 3})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsKind(err, ErrKindProviderNotFound) {
		t.Fatalf("err=%v want kind=%v", err, ErrKindProviderNotFound)
	}
}

func TestOrchestrator_SearchTimeout(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register("slow", func(httpClient *http.Client) Provider {
		return &programmableProvider{
			info:  ProviderInfo{ID: "slow", Name: "slow"},
			delay: 200 * time.Millisecond,
		}
	})
	o := NewOrchestrator(reg, http.DefaultClient, WithTimeout(50*time.Millisecond))

	_, err := o.Search(context.Background(), "slow", ProviderConfig{}, "hello", SearchConfig{MaxResults: 3})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsKind(err, ErrKindTimeout) {
		t.Fatalf("err=%v want kind=%v", err, ErrKindTimeout)
	}
}

func TestOrchestrator_ExcludeDomainsFilterAndMaxResultsTrim(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register("static", func(httpClient *http.Client) Provider {
		return &programmableProvider{
			info: ProviderInfo{ID: "static", Name: "static"},
			resp: &SearchResponse{
				Query: "hello",
				Results: []SearchResult{
					{Title: "a", URL: "https://example.com/a", Content: "1"},
					{Title: "b", URL: "https://sub.example.com/b", Content: "2"},
					{Title: "c", URL: "https://other.com/c", Content: "3"},
				},
			},
		}
	})
	o := NewOrchestrator(reg, http.DefaultClient, WithTimeout(2*time.Second))

	resp, err := o.Search(context.Background(), "static", ProviderConfig{}, "hello", SearchConfig{
		MaxResults:     1,
		ExcludeDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if resp == nil {
		t.Fatalf("resp=nil")
	}
	if len(resp.Results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(resp.Results))
	}
	if resp.Results[0].URL != "https://other.com/c" {
		t.Fatalf("url=%q want other.com result", resp.Results[0].URL)
	}
}

func TestOrchestrator_SeparatesFetchResultsFromFinalResults(t *testing.T) {
	reg := NewRegistry()
	provider := &programmableProvider{
		info: ProviderInfo{ID: "static", Name: "static"},
		resp: &SearchResponse{
			Query: "hello",
			Results: []SearchResult{
				{Title: "a", URL: "https://a.com/1", Content: "1"},
				{Title: "b", URL: "https://a.com/2", Content: "2"},
				{Title: "c", URL: "https://a.com/3", Content: "3"},
			},
		},
	}
	_ = reg.Register("static", func(httpClient *http.Client) Provider {
		return provider
	})
	o := NewOrchestrator(reg, http.DefaultClient, WithTimeout(2*time.Second))

	resp, err := o.Search(context.Background(), "static", ProviderConfig{}, "hello", SearchConfig{
		MaxResults:   2,
		FetchResults: 4,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if provider.lastCfg.MaxResults != 2 {
		t.Fatalf("provider max_results=%d want 2", provider.lastCfg.MaxResults)
	}
	if provider.lastCfg.FetchResults != 4 {
		t.Fatalf("provider fetch_results=%d want 4", provider.lastCfg.FetchResults)
	}
	if resp == nil {
		t.Fatalf("resp=nil")
	}
	if len(resp.Results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(resp.Results))
	}
}

func TestOrchestrator_EmptyResults(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register("empty", func(httpClient *http.Client) Provider {
		return &programmableProvider{
			info: ProviderInfo{ID: "empty", Name: "empty"},
			resp: &SearchResponse{Query: "hello", Results: nil},
		}
	})
	o := NewOrchestrator(reg, http.DefaultClient, WithTimeout(2*time.Second))

	resp, err := o.Search(context.Background(), "empty", ProviderConfig{}, "hello", SearchConfig{MaxResults: 3})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if resp == nil {
		t.Fatalf("resp=nil")
	}
	if len(resp.Results) != 0 {
		t.Fatalf("len(results)=%d want 0", len(resp.Results))
	}
}
