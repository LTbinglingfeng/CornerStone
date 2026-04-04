package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBocha_Search_RequestAndResponseMapping(t *testing.T) {
	var gotReq bochaSearchRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/v1/web-search" {
			t.Fatalf("path=%s want /v1/web-search", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test_key" {
			t.Fatalf("Authorization=%q want %q", auth, "Bearer test_key")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type=%q want application/json", ct)
		}

		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": "ok",
			"data": {
				"queryContext": { "originalQuery": "hello" },
				"webPages": {
					"value": [
						{ "name": "t1", "url": "https://a.com/1", "snippet": "s1", "summary": "sum1" },
						{ "name": "t2", "url": "https://b.com/2", "snippet": "s2" }
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	provider := NewBocha(srv.Client())
	resp, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:     2,
		FetchResults:   4,
		ExcludeDomains: []string{"x.com", "y.com"},
		SearchWithTime: true,
	}, search.ProviderConfig{
		APIKey:  "test_key",
		APIHost: srv.URL,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotReq.Query != "hello" {
		t.Fatalf("req.query=%q want hello", gotReq.Query)
	}
	if gotReq.Count != 4 {
		t.Fatalf("req.count=%d want 4", gotReq.Count)
	}
	if gotReq.Exclude != "x.com,y.com" {
		t.Fatalf("req.exclude=%q want x.com,y.com", gotReq.Exclude)
	}
	if gotReq.Freshness != "oneWeek" {
		t.Fatalf("req.freshness=%q want oneWeek", gotReq.Freshness)
	}
	if gotReq.Page != 1 {
		t.Fatalf("req.page=%d want 1", gotReq.Page)
	}
	if !gotReq.Summary {
		t.Fatalf("req.summary=false want true")
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
	if resp.Results[0].Content != "sum1" {
		t.Fatalf("result[0].content=%q want sum1", resp.Results[0].Content)
	}
	if resp.Results[1].Content != "s2" {
		t.Fatalf("result[1].content=%q want s2", resp.Results[1].Content)
	}
}
