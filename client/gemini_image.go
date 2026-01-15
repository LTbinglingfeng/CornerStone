package client

import (
	"context"
	"cornerstone/logging"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"
)

const imageGenerationTimeout = 5 * time.Minute

// GeminiImageClient Google Gemini 生图（Imagen）客户端
type GeminiImageClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	genaiBaseURL string
	genaiVersion string
}

func NewGeminiImageClient(baseURL, apiKey string) *GeminiImageClient {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	genaiBaseURL, genaiVersion := normalizeGeminiBaseURL(baseURL)
	return &GeminiImageClient{
		BaseURL:      strings.TrimSuffix(baseURL, "/"),
		APIKey:       apiKey,
		HTTPClient:   newHTTPClient(),
		genaiBaseURL: genaiBaseURL,
		genaiVersion: genaiVersion,
	}
}

func (c *GeminiImageClient) newGenAIClient(ctx context.Context) (*genai.Client, error) {
	cfg := &genai.ClientConfig{
		APIKey:     c.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: c.HTTPClient,
	}
	if c.genaiBaseURL != "" {
		cfg.HTTPOptions.BaseURL = c.genaiBaseURL
	}
	if c.genaiVersion != "" {
		cfg.HTTPOptions.APIVersion = c.genaiVersion
	}
	return genai.NewClient(ctx, cfg)
}

func (c *GeminiImageClient) GenerateImages(ctx context.Context, model, prompt string, cfg *genai.GenerateImagesConfig) (*genai.GenerateImagesResponse, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, imageGenerationTimeout)
	defer cancel()

	genaiClient, errInit := c.newGenAIClient(ctxTimeout)
	if errInit != nil {
		logging.Errorf("gemini image init failed: model=%s err=%v", model, errInit)
		return nil, fmt.Errorf("init genai client: %w", errInit)
	}

	resp, err := genaiClient.Models.GenerateImages(ctxTimeout, model, prompt, cfg)
	if err != nil {
		translated := translateGenAIGenerateImagesError(err)
		logging.Errorf("gemini image request failed: model=%s err=%v", model, translated)
		return nil, translated
	}
	if resp == nil || len(resp.GeneratedImages) == 0 {
		logging.Warnf("gemini image empty response: model=%s prompt=%s", model, logging.Truncate(prompt, 100))
	}
	return resp, nil
}

func translateGenAIGenerateImagesError(err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			msg = strings.TrimSpace(apiErr.Status)
		}
		if msg == "" {
			msg = apiErr.Error()
		}
		return fmt.Errorf("API error (status %d): %s", apiErr.Code, msg)
	}
	return fmt.Errorf("generate images: %w", err)
}
