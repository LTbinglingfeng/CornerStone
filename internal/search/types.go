package search

// SearchConfig controls result shaping and optional filters.
// The model only supplies `query`; everything else comes from user/system settings.
type SearchConfig struct {
	MaxResults     int      `json:"max_results"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	SearchWithTime bool     `json:"search_with_time,omitempty"`
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// ProviderConfig is the normalized provider runtime config.
// It intentionally stays generic so the model never needs to know provider differences.
type ProviderConfig struct {
	APIKey            string
	APIHost           string
	BasicAuthUsername string
	BasicAuthPassword string
}

type ProviderInfo struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	RequiresAPIKey       bool   `json:"requires_api_key"`
	RequiresAPIHost      bool   `json:"requires_api_host"`
	SupportsExclude      bool   `json:"supports_exclude_domains"`
	SupportsTimeFilter   bool   `json:"supports_time_filter"`
	SupportsBasicAuth    bool   `json:"supports_basic_auth"`
	SupportsMaxResults   bool   `json:"supports_max_results"`
	SupportsSearchConfig bool   `json:"supports_search_config"`
}
