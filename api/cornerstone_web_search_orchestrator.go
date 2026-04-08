package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/internal/search"
	"cornerstone/internal/search/providers"
	"cornerstone/logging"
	"time"
)

func newCornerstoneWebSearchOrchestrator(cfg config.Config) *search.Orchestrator {
	reg := search.NewRegistry()
	if errRegister := providers.RegisterAll(reg); errRegister != nil {
		logging.Errorf("%s register providers failed: %v", cornerstoneWebSearchToolName, errRegister)
		return nil
	}

	timeout := time.Duration(cfg.CornerstoneWebSearch.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = search.DefaultTimeout
	}

	return search.NewOrchestrator(
		reg,
		client.NewHTTPClient(),
		search.WithTimeout(timeout),
	)
}
