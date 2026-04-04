package search

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type Registry struct {
	factories map[string]ProviderFactory
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]ProviderFactory),
	}
}

func (r *Registry) Register(providerID string, factory ProviderFactory) error {
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	id := strings.TrimSpace(providerID)
	if id == "" {
		return fmt.Errorf("missing provider id")
	}
	if factory == nil {
		return fmt.Errorf("missing provider factory: id=%s", id)
	}
	if _, exists := r.factories[id]; exists {
		return fmt.Errorf("provider already registered: id=%s", id)
	}
	r.factories[id] = factory
	return nil
}

func (r *Registry) Has(providerID string) bool {
	if r == nil {
		return false
	}
	_, ok := r.factories[strings.TrimSpace(providerID)]
	return ok
}

func (r *Registry) Create(providerID string, httpClient *http.Client) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("nil registry")
	}
	id := strings.TrimSpace(providerID)
	if id == "" {
		return nil, fmt.Errorf("missing provider id")
	}
	factory, ok := r.factories[id]
	if !ok {
		return nil, &Error{Kind: ErrKindProviderNotFound, ProviderID: id, Message: "provider not found"}
	}
	provider := factory(httpClient)
	if provider == nil {
		return nil, fmt.Errorf("provider factory returned nil: id=%s", id)
	}
	return provider, nil
}

func (r *Registry) Infos(httpClient *http.Client) []ProviderInfo {
	if r == nil {
		return nil
	}
	infos := make([]ProviderInfo, 0, len(r.factories))
	for _, factory := range r.factories {
		provider := factory(httpClient)
		if provider == nil {
			continue
		}
		info := provider.Info()
		if strings.TrimSpace(info.ID) == "" {
			continue
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ID < infos[j].ID
	})
	return infos
}
