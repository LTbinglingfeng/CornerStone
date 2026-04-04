package providers

import (
	"bytes"
	"context"
	"cornerstone/internal/search"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const maxResponseBodyBytes = 2 << 20 // 2MB

func resolveEndpoint(apiHost string, relativePath string) (string, error) {
	raw := strings.TrimSpace(apiHost)
	if raw == "" {
		return "", fmt.Errorf("api_host is required")
	}

	parsed, errParse := url.Parse(raw)
	if errParse != nil {
		return "", fmt.Errorf("invalid api_host: %w", errParse)
	}
	if parsed.Scheme == "" {
		parsed, errParse = url.Parse("https://" + raw)
		if errParse != nil {
			return "", fmt.Errorf("invalid api_host: %w", errParse)
		}
	}

	rel := strings.TrimSpace(relativePath)
	if rel == "" {
		return parsed.String(), nil
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return parsed.String(), nil
	}

	existing := strings.TrimSuffix(parsed.Path, "/")
	relPath := "/" + rel
	if existing == relPath || strings.HasSuffix(existing, relPath) {
		return parsed.String(), nil
	}

	parsed.Path = path.Join(existing, rel)
	return parsed.String(), nil
}

func basicAuthHeader(username, password string) string {
	if strings.TrimSpace(username) == "" {
		return ""
	}
	token := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Basic " + token
}

func providerFetchResults(cfg search.SearchConfig) int {
	count := cfg.FetchResults
	if count <= 0 {
		count = cfg.MaxResults
	}
	if count <= 0 {
		count = 1
	}
	return count
}

func doJSON(ctx context.Context, httpClient *http.Client, method, endpoint string, headers map[string]string, body any, out any) (*http.Response, []byte, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var payload io.Reader
	if body != nil {
		data, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return nil, nil, errMarshal
		}
		payload = bytes.NewReader(data)
	}

	req, errReq := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if errReq != nil {
		return nil, nil, errReq
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, errDo := httpClient.Do(req)
	if errDo != nil {
		return nil, nil, errDo
	}
	defer func() { _ = resp.Body.Close() }()

	data, errRead := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBodyBytes)+1))
	if errRead != nil {
		return resp, nil, errRead
	}
	if len(data) > maxResponseBodyBytes {
		return resp, data[:maxResponseBodyBytes], fmt.Errorf("response body too large")
	}
	if out != nil {
		if errUnmarshal := json.Unmarshal(data, out); errUnmarshal != nil {
			return resp, data, errUnmarshal
		}
	}
	return resp, data, nil
}
