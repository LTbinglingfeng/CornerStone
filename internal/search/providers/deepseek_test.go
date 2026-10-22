package providers

import (
	"context"
	"cornerstone/internal/search"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSeekRequestAndMapping(t *testing.T) {
	for _, basePath := range []string{"", "/v1", "/v1/", "/v1/tools/search"} {
		t.Run(basePath, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/tools/search" {
					t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("unexpected headers")
				}
				var body deepSeekSearchRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Query != "搜索" || body.Count != 50 || body.NeedContent {
					t.Errorf("unexpected request: %+v", body)
				}
				_, _ = w.Write([]byte(`{"results":[{"title":" title ","url":" https://example.com ","summary":" summary ","snippet":"snippet","content":"body","date":null,"site_name":null},{"snippet":" fallback "},{"content":" body "}]}`))
			}))
			defer srv.Close()
			got, err := NewDeepSeek(srv.Client()).Search(context.Background(), " 搜索 ", search.SearchConfig{MaxResults: 2, FetchResults: 99}, search.ProviderConfig{APIKey: " test-key ", APIHost: srv.URL + basePath})
			if err != nil {
				t.Fatal(err)
			}
			if got.Query != "搜索" || len(got.Results) != 3 || got.Results[0].Title != "title" || got.Results[0].URL != "https://example.com" || got.Results[0].Content != "summary" || got.Results[1].Content != "fallback" || got.Results[2].Content != "body" {
				t.Fatalf("unexpected response: %+v", got)
			}
		})
	}
}

func TestDeepSeekValidation(t *testing.T) {
	for _, tc := range []struct {
		query, key string
		kind       search.ErrorKind
	}{
		{" ", "key", search.ErrKindInvalidRequest},
		{strings.Repeat("中", 129), "key", search.ErrKindInvalidRequest},
		{"query", " ", search.ErrKindProviderNotConfigured},
	} {
		_, err := NewDeepSeek(nil).Search(context.Background(), tc.query, search.SearchConfig{}, search.ProviderConfig{APIKey: tc.key})
		var se *search.Error
		if !errors.As(err, &se) || se.Kind != tc.kind {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestDeepSeekResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   search.ErrorKind
	}{
		{"empty", 200, `{"results":[]}`, ""},
		{"malformed", 200, `not json`, search.ErrKindBadResponse},
		{"missing", 200, `{}`, search.ErrKindBadResponse},
		{"null", 200, `null`, search.ErrKindBadResponse},
		{"unauthorized", 401, `{"error":{"message":"secret"}}`, search.ErrKindUpstream},
		{"rate limit", 429, `rate limited`, search.ErrKindUpstream},
		{"server error", 500, `<html>error</html>`, search.ErrKindUpstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body deepSeekSearchRequest
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.Count != 1 || len([]rune(body.Query)) != 128 {
					t.Errorf("unexpected request: %+v", body)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got, err := NewDeepSeek(srv.Client()).Search(context.Background(), strings.Repeat("中", 128), search.SearchConfig{}, search.ProviderConfig{APIKey: "key", APIHost: srv.URL})
			if tc.kind == "" {
				if err != nil || len(got.Results) != 0 {
					t.Fatalf("got %v, %v", got, err)
				}
				return
			}
			var se *search.Error
			if !errors.As(err, &se) || se.Kind != tc.kind || se.ProviderID != ProviderIDDeepSeek {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.status != 200 && se.StatusCode != tc.status {
				t.Errorf("status=%d", se.StatusCode)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Error("upstream body leaked")
			}
		})
	}
}

func TestDeepSeekRegistrationAndOrchestration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"url":"https://blocked.example/a"},{"url":"https://allowed.example/a","summary":"kept"},{"url":"https://allowed.example/b"}]}`))
	}))
	defer srv.Close()
	registry := search.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	p, err := registry.Create(ProviderIDDeepSeek, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	info := p.Info()
	if !info.RequiresAPIKey || !info.SupportsMaxResults || info.SupportsTimeFilter || info.SupportsExclude {
		t.Fatalf("unexpected capabilities: %+v", info)
	}
	got, err := search.NewOrchestrator(registry, srv.Client()).Search(context.Background(), ProviderIDDeepSeek, search.ProviderConfig{APIKey: "key", APIHost: srv.URL}, "query", search.SearchConfig{MaxResults: 1, ExcludeDomains: []string{"blocked.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || got.Results[0].Content != "kept" {
		t.Fatalf("unexpected results: %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Search(ctx, "query", search.SearchConfig{}, search.ProviderConfig{APIKey: "key", APIHost: srv.URL})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
