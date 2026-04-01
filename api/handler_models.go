package api

import (
	"cornerstone/config"
	"cornerstone/logging"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// modelsRequest 获取模型列表的请求体
type modelsRequest struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

func normalizeModelFetchBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func canReuseSavedModelFetchAPIKey(saved *config.Provider, req modelsRequest) bool {
	if saved == nil || strings.TrimSpace(saved.APIKey) == "" {
		return false
	}
	if saved.Type != config.ProviderType(req.Type) {
		return false
	}
	return normalizeModelFetchBaseURL(saved.BaseURL) == normalizeModelFetchBaseURL(req.BaseURL)
}

// handleProviderModels 获取供应商可用模型列表
func (h *Handler) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonResponse(w, http.StatusMethodNotAllowed, Response{Success: false, Error: "Method not allowed"})
		return
	}

	// 从路径提取 provider ID: /management/providers/{id}/models
	path := strings.TrimPrefix(r.URL.Path, "/management/providers/")
	path = strings.TrimSuffix(path, "/models")
	id := strings.TrimSpace(path)
	if id == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Provider ID required"})
		return
	}

	// 解析请求体中的当前表单值
	var req modelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "Invalid request body"})
		return
	}

	req.Type = strings.TrimSpace(req.Type)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.APIKey = strings.TrimSpace(req.APIKey)

	if req.BaseURL == "" || req.Type == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "type and base_url are required"})
		return
	}

	// 仅当当前表单配置仍与已保存配置一致时，才允许复用已保存的 key。
	apiKey := req.APIKey
	if apiKey == "" {
		saved := h.configManager.GetProvider(id)
		if canReuseSavedModelFetchAPIKey(saved, req) {
			apiKey = saved.APIKey
		}
	}
	if apiKey == "" {
		h.jsonResponse(w, http.StatusBadRequest, Response{Success: false, Error: "api_key is required"})
		return
	}

	provider := &config.Provider{
		Type:    config.ProviderType(req.Type),
		BaseURL: req.BaseURL,
		APIKey:  apiKey,
	}

	models, err := fetchModelsFromProvider(provider)
	if err != nil {
		logging.Errorf("fetch_models provider_id=%s error=%s", id, err.Error())
		h.jsonResponse(w, http.StatusBadGateway, Response{Success: false, Error: err.Error()})
		return
	}

	sort.Strings(models)
	h.jsonResponse(w, http.StatusOK, Response{Success: true, Data: map[string]interface{}{
		"models": models,
	}})
}

// fetchModelsFromProvider 从供应商 API 获取模型列表
func fetchModelsFromProvider(provider *config.Provider) ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	var req *http.Request
	var err error

	switch provider.Type {
	case config.ProviderTypeOpenAI, config.ProviderTypeOpenAIResponse:
		url := strings.TrimRight(provider.BaseURL, "/") + "/models"
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	case config.ProviderTypeAnthropic:
		url := strings.TrimRight(provider.BaseURL, "/") + "/models"
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")

	case config.ProviderTypeGemini, config.ProviderTypeGeminiImage:
		url := strings.TrimRight(provider.BaseURL, "/") + "/models?key=" + provider.APIKey
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported provider type: %s", provider.Type)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream API returned %d: %s", resp.StatusCode, truncateBody(body, 200))
	}

	return parseModelsResponse(provider.Type, body)
}

// parseModelsResponse 解析不同供应商的模型列表响应
func parseModelsResponse(providerType config.ProviderType, body []byte) ([]string, error) {
	switch providerType {
	case config.ProviderTypeOpenAI, config.ProviderTypeOpenAIResponse, config.ProviderTypeAnthropic:
		return parseOpenAIStyleModels(body)
	case config.ProviderTypeGemini, config.ProviderTypeGeminiImage:
		return parseGeminiModels(body)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}
}

// parseOpenAIStyleModels 解析 OpenAI/Anthropic 格式 {"data": [{"id": "..."}]}
func parseOpenAIStyleModels(body []byte) ([]string, error) {
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		id := strings.TrimSpace(m.ID)
		if id != "" {
			models = append(models, id)
		}
	}
	return models, nil
}

// parseGeminiModels 解析 Gemini 格式 {"models": [{"name": "models/..."}]}
func parseGeminiModels(body []byte) ([]string, error) {
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			continue
		}
		// 去掉 "models/" 前缀
		name = strings.TrimPrefix(name, "models/")
		models = append(models, name)
	}
	return models, nil
}

// truncateBody 截断响应体用于错误消息
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
