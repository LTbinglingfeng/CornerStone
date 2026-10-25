package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	httpClient := &http.Client{Timeout: 15 * time.Second}

	switch provider.Type {
	case config.ProviderTypeOpenAI, config.ProviderTypeOpenAIResponse:
		endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
		body, err := doModelsRequest(httpClient, endpoint, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		})
		if err != nil {
			return nil, err
		}
		return parseOpenAIStyleModels(body)

	case config.ProviderTypeAnthropic:
		endpoint := strings.TrimRight(provider.BaseURL, "/") + "/models"
		body, err := doModelsRequest(httpClient, endpoint, func(req *http.Request) {
			req.Header.Set("x-api-key", provider.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		})
		if err != nil {
			return nil, err
		}
		return parseOpenAIStyleModels(body)

	case config.ProviderTypeGemini, config.ProviderTypeGeminiImage:
		// 与聊天客户端一致：base URL 可带或不带版本段（未带时使用 v1beta）。
		endpoint := client.GeminiModelsListURL(provider.BaseURL)
		models := make([]string, 0)
		pageToken := ""
		for page := 0; page < 10; page++ {
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, fmt.Errorf("invalid base URL: %w", err)
			}
			q := u.Query()
			q.Set("pageSize", "1000")
			if pageToken != "" {
				q.Set("pageToken", pageToken)
			}
			u.RawQuery = q.Encode()
			body, err := doModelsRequest(httpClient, u.String(), func(req *http.Request) {
				req.Header.Set("x-goog-api-key", provider.APIKey)
			})
			if err != nil {
				return nil, err
			}
			pageModels, next, err := parseGeminiModelsPage(body)
			if err != nil {
				return nil, err
			}
			models = append(models, pageModels...)
			if next == "" {
				break
			}
			pageToken = next
		}
		return models, nil

	default:
		return nil, fmt.Errorf("unsupported provider type: %s", provider.Type)
	}
}

// doModelsRequest 发起 GET 请求并返回响应体；非 200 时返回带请求地址的可读错误。
func doModelsRequest(httpClient *http.Client, endpoint string, setHeaders func(*http.Request)) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setHeaders(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		displayURL := endpoint
		if i := strings.IndexByte(displayURL, '?'); i >= 0 {
			displayURL = displayURL[:i]
		}
		msg := strings.TrimSpace(truncateBody(body, 200))
		if resp.StatusCode == http.StatusNotFound {
			hint := "please check the API URL"
			if msg != "" {
				hint = msg + "; " + hint
			}
			return nil, fmt.Errorf("upstream API returned 404 for GET %s (%s)", displayURL, hint)
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("upstream API returned %d for GET %s: %s", resp.StatusCode, displayURL, msg)
	}
	return body, nil
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
	models, _, err := parseGeminiModelsPage(body)
	return models, err
}

// parseGeminiModelsPage 解析单页 Gemini 模型列表，并返回下一页 token。
func parseGeminiModelsPage(body []byte) ([]string, string, error) {
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %w", err)
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
	return models, strings.TrimSpace(result.NextPageToken), nil
}

// truncateBody 截断响应体用于错误消息
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
