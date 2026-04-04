package search

import (
	"context"
	"net/http"
	"testing"
)

type stubProvider struct {
	id string
}

func (p *stubProvider) Info() ProviderInfo {
	return ProviderInfo{ID: p.id, Name: "stub"}
}

func (p *stubProvider) Search(ctx context.Context, query string, cfg SearchConfig, providerCfg ProviderConfig) (*SearchResponse, error) {
	return &SearchResponse{Query: query, Results: nil}, nil
}

func TestRegistry_RegisterAndCreate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("stub", func(httpClient *http.Client) Provider { return &stubProvider{id: "stub"} }); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if !reg.Has("stub") {
		t.Fatalf("Has(stub)=false")
	}
	provider, err := reg.Create("stub", nil)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if provider == nil || provider.Info().ID != "stub" {
		t.Fatalf("provider id=%q, want stub", provider.Info().ID)
	}
}

func TestRegistry_RegisterDuplicateRejected(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("stub", func(httpClient *http.Client) Provider { return &stubProvider{id: "stub"} }); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if err := reg.Register("stub", func(httpClient *http.Client) Provider { return &stubProvider{id: "stub"} }); err == nil {
		t.Fatalf("expected duplicate register error")
	}
}

func TestRegistry_CreateNotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Create("missing", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsKind(err, ErrKindProviderNotFound) {
		t.Fatalf("err kind=%v want %v", err, ErrKindProviderNotFound)
	}
}
