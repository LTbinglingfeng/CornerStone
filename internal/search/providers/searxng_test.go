package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSearxNG_Search_RequestAndResponseMapping(t *testing.T) {
	var gotAuth string
	var gotQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s want GET", r.Method)
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path=%s want /search", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "hello",
			"results": []map[string]string{
				{"title": "t1", "url": "https://a.com/1", "content": "c1"},
				{"title": "t2", "url": "https://b.com/2", "content": "c2"},
			},
		})
	}))
	defer srv.Close()

	provider := NewSearxNG(srv.Client())
	resp, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:     2,
		FetchResults:   4,
		SearchWithTime: true,
	}, search.ProviderConfig{
		APIHost:           srv.URL,
		BasicAuthUsername: "alice",
		BasicAuthPassword: "secret",
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotQuery.Get("format") != "json" {
		t.Fatalf("format=%q want json", gotQuery.Get("format"))
	}
	if gotQuery.Get("q") != "hello" {
		t.Fatalf("q=%q want hello", gotQuery.Get("q"))
	}
	if gotQuery.Get("language") != "auto" {
		t.Fatalf("language=%q want auto", gotQuery.Get("language"))
	}
	if gotQuery.Get("time_range") != "month" {
		t.Fatalf("time_range=%q want month", gotQuery.Get("time_range"))
	}
	if gotAuth != basicAuthHeader("alice", "secret") {
		t.Fatalf("Authorization=%q want %q", gotAuth, basicAuthHeader("alice", "secret"))
	}

	if resp == nil {
		t.Fatal("resp=nil")
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
