package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTavily_Search_RequestAndResponseMapping(t *testing.T) {
	var gotReq tavilySearchRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path=%s want /search", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "hello",
			"results": [
				{ "title": "t1", "url": "https://a.com/1", "content": "c1" },
				{ "title": "t2", "url": "https://b.com/2", "content": "c2" }
			]
		}`))
	}))
	defer srv.Close()

	provider := NewTavily(srv.Client())
	resp, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:   2,
		FetchResults: 4,
	}, search.ProviderConfig{
		APIKey:  "test_key",
		APIHost: srv.URL,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotReq.APIKey != "test_key" {
		t.Fatalf("req.api_key=%q want test_key", gotReq.APIKey)
	}
	if gotReq.Query != "hello" {
		t.Fatalf("req.query=%q want hello", gotReq.Query)
	}
	if gotReq.MaxResults != 4 {
		t.Fatalf("req.max_results=%d want 4", gotReq.MaxResults)
	}

	if resp == nil {
		t.Fatalf("resp=nil")
	}
	if resp.Query != "hello" {
		t.Fatalf("resp.query=%q want hello", resp.Query)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(resp.Results))
	}
	if resp.Results[0].Title != "t1" || resp.Results[0].URL != "https://a.com/1" || resp.Results[0].Content != "c1" {
		t.Fatalf("unexpected result[0]=%#v", resp.Results[0])
	}
}
