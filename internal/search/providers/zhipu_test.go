package providers

import (
	"bytes"
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestZhipu_Search_RequestAndResponseMapping(t *testing.T) {
	var gotReq zhipuWebSearchRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/paas/v4/web_search" {
			t.Fatalf("path=%s want /paas/v4/web_search", r.URL.Path)
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
			"search_result": [
				{ "title": "t1", "content": "c1", "link": "https://a.com/1" },
				{ "title": "t2", "content": "c2", "link": "https://b.com/2" }
			]
		}`))
	}))
	defer srv.Close()

	provider := NewZhipu(srv.Client())
	resp, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:     2,
		FetchResults:   4,
		SearchWithTime: true,
	}, search.ProviderConfig{
		APIKey:       "test_key",
		APIHost:      srv.URL,
		SearchEngine: ZhipuSearchEngineProQuark,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotReq.SearchQuery != "hello" {
		t.Fatalf("req.search_query=%q want hello", gotReq.SearchQuery)
	}
	if gotReq.SearchEngine != ZhipuSearchEngineProQuark {
		t.Fatalf("req.search_engine=%q want %q", gotReq.SearchEngine, ZhipuSearchEngineProQuark)
	}
	if gotReq.SearchIntent {
		t.Fatalf("req.search_intent=true want false")
	}
	if gotReq.Count != 4 {
		t.Fatalf("req.count=%d want 4", gotReq.Count)
	}
	if gotReq.SearchRecencyFilter != "oneWeek" {
		t.Fatalf("req.search_recency_filter=%q want oneWeek", gotReq.SearchRecencyFilter)
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
	if resp.Results[0].Title != "t1" || resp.Results[0].Content != "c1" || resp.Results[0].URL != "https://a.com/1" {
		t.Fatalf("result[0]=%+v want mapped fields", resp.Results[0])
	}
}

func TestZhipu_Search_DefaultHostUsesOfficialEndpoint(t *testing.T) {
	type capture struct {
		method string
		url    string
		auth   string
		body   zhipuWebSearchRequest
	}

	var got capture
	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			got.method = r.Method
			got.url = r.URL.String()
			got.auth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&got.body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewBufferString(`{"search_result":[]}`)),
				Request:    r,
			}, nil
		}),
	}

	provider := NewZhipu(client)
	_, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:   3,
		FetchResults: 3,
	}, search.ProviderConfig{
		APIKey: "test_key",
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if got.method != http.MethodPost {
		t.Fatalf("method=%s want POST", got.method)
	}
	if got.url != "https://open.bigmodel.cn/api/paas/v4/web_search" {
		t.Fatalf("url=%q want %q", got.url, "https://open.bigmodel.cn/api/paas/v4/web_search")
	}
	if got.auth != "Bearer test_key" {
		t.Fatalf("Authorization=%q want %q", got.auth, "Bearer test_key")
	}
	if got.body.SearchEngine != ZhipuSearchEngineStd {
		t.Fatalf("req.search_engine=%q want %q", got.body.SearchEngine, ZhipuSearchEngineStd)
	}
	if got.body.SearchRecencyFilter != "noLimit" {
		t.Fatalf("req.search_recency_filter=%q want noLimit", got.body.SearchRecencyFilter)
	}
}

func TestZhipu_Search_FullEndpointIsNotDuplicated(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"search_result":[]}`))
	}))
	defer srv.Close()

	provider := NewZhipu(srv.Client())
	_, err := provider.Search(context.Background(), "hello", search.SearchConfig{}, search.ProviderConfig{
		APIKey:  "test_key",
		APIHost: srv.URL + "/paas/v4/web_search",
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotPath != "/paas/v4/web_search" {
		t.Fatalf("path=%q want /paas/v4/web_search", gotPath)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
