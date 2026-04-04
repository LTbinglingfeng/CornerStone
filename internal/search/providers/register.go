package providers

import (
	"cornerstone/internal/search"
	"net/http"
)

// RegisterAll registers all built-in web search providers into the registry.
func RegisterAll(registry *search.Registry) error {
	if registry == nil {
		return nil
	}

	providers := []struct {
		id      string
		factory search.ProviderFactory
	}{
		{id: ProviderIDExa, factory: func(httpClient *http.Client) search.Provider { return NewExa(httpClient) }},
		{id: ProviderIDTavily, factory: func(httpClient *http.Client) search.Provider { return NewTavily(httpClient) }},
		{id: ProviderIDBocha, factory: func(httpClient *http.Client) search.Provider { return NewBocha(httpClient) }},
		{id: ProviderIDQuerit, factory: func(httpClient *http.Client) search.Provider { return NewQuerit(httpClient) }},
		{id: ProviderIDZhipu, factory: func(httpClient *http.Client) search.Provider { return NewZhipu(httpClient) }},
		{id: ProviderIDSearxNG, factory: func(httpClient *http.Client) search.Provider { return NewSearxNG(httpClient) }},
	}

	for _, item := range providers {
		if err := registry.Register(item.id, item.factory); err != nil {
			return err
		}
	}
	return nil
}
