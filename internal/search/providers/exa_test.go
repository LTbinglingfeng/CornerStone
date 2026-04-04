package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExa_Search_RequestAndResponseMapping(t *testing.T) {
	var gotReq exaSearchRequest
	var gotAPIKey string
	var gotAuthorization string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want POST", r.Method)
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path=%s want /search", r.URL.Path)
		}
		gotAPIKey = r.Header.Get("x-api-key")
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"autopromptString": "hello",
			"results": [
				{ "title": "t1", "url": "https://a.com/1", "text": "c1" },
				{ "title": "t2", "url": "https://b.com/2", "text": "c2" }
			]
		}`))
	}))
	defer srv.Close()

	provider := NewExa(srv.Client())
	resp, err := provider.Search(context.Background(), "hello", search.SearchConfig{
		MaxResults:     2,
		FetchResults:   4,
		ExcludeDomains: []string{"example.com", "news.example.com"},
		SearchWithTime: true,
	}, search.ProviderConfig{
		APIKey:  "test_key",
		APIHost: srv.URL,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if gotAPIKey != "test_key" {
		t.Fatalf("x-api-key=%q want test_key", gotAPIKey)
	}
	if gotAuthorization != "" {
		t.Fatalf("Authorization=%q want empty", gotAuthorization)
	}
	if gotReq.Query != "hello" {
		t.Fatalf("req.query=%q want hello", gotReq.Query)
	}
	if gotReq.NumResults != 4 {
		t.Fatalf("req.numResults=%d want 4", gotReq.NumResults)
	}
	if !gotReq.Contents.Text {
		t.Fatal("req.contents.text=false want true")
	}
	if len(gotReq.ExcludeDomains) != 2 || gotReq.ExcludeDomains[0] != "example.com" || gotReq.ExcludeDomains[1] != "news.example.com" {
		t.Fatalf("req.excludeDomains=%v want both domains", gotReq.ExcludeDomains)
	}
	if gotReq.StartPublishedDate == "" {
		t.Fatal("req.startPublishedDate empty")
	}
	if _, err := time.Parse("2006-01-02", gotReq.StartPublishedDate); err != nil {
		t.Fatalf("req.startPublishedDate=%q not parseable date: %v", gotReq.StartPublishedDate, err)
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
