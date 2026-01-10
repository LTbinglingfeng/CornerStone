package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GeminiClient Google Gemini API 客户端
type GeminiClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// Gemini API 请求结构
type GeminiRequest struct {
	Contents          []GeminiContent         `json:"contents"`
	SystemInstruction *GeminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

// Gemini API 响应结构
type GeminiResponse struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata *GeminiUsage      `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Index        int           `json:"index"`
}

type GeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Gemini 流式响应
type GeminiStreamResponse struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata *GeminiUsage      `json:"usageMetadata,omitempty"`
}

// NewGeminiClient 创建新的Gemini客户端
func NewGeminiClient(baseURL, apiKey string) *GeminiClient {
	// 默认使用 Google Gemini API 地址
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiClient{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: newHTTPClient(),
	}
}

// convertToGeminiMessages 将通用消息转换为Gemini格式
func (c *GeminiClient) convertToGeminiMessages(messages []Message) ([]GeminiContent, *GeminiContent) {
	var contents []GeminiContent
	var systemInstruction *GeminiContent

	for _, msg := range messages {
		if msg.Role == "system" {
			// Gemini 使用 systemInstruction 字段处理系统消息
			systemInstruction = &GeminiContent{
				Parts: []GeminiPart{{Text: msg.Content}},
			}
			continue
		}

		role := msg.Role
		// Gemini 使用 "user" 和 "model" 而不是 "assistant"
		if role == "assistant" {
			role = "model"
		}

		contents = append(contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: msg.Content}},
		})
	}

	return contents, systemInstruction
}

// Chat 发送聊天请求（非流式）
func (c *GeminiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	contents, systemInstruction := c.convertToGeminiMessages(req.Messages)

	ctx, cancel := context.WithTimeout(ctx, chatRequestTimeout)
	defer cancel()

	geminiReq := GeminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
	}

	if req.Temperature > 0 || req.MaxTokens > 0 {
		geminiReq.GenerationConfig = &GeminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// 构建请求URL
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.BaseURL, req.Model, c.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// 转换为通用响应格式
	return c.convertToOpenAIResponse(geminiResp, req.Model), nil
}

// ChatStream 发送聊天请求（流式）
func (c *GeminiClient) ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error {
	contents, systemInstruction := c.convertToGeminiMessages(req.Messages)

	ctx, cancel := context.WithTimeout(ctx, streamRequestTimeout)
	defer cancel()

	geminiReq := GeminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
	}

	if req.Temperature > 0 || req.MaxTokens > 0 {
		geminiReq.GenerationConfig = &GeminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// 构建流式请求URL
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse", c.BaseURL, req.Model, c.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxStreamLineBytes)
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var geminiResp GeminiStreamResponse
		if err := json.Unmarshal([]byte(data), &geminiResp); err != nil {
			continue
		}

		// 转换为通用流式响应块
		chunk := c.convertToStreamChunk(geminiResp, req.Model)
		if err := callback(chunk); err != nil {
			return err
		}
	}

	return scanner.Err()
}

// convertToOpenAIResponse 将Gemini响应转换为OpenAI格式
func (c *GeminiClient) convertToOpenAIResponse(resp GeminiResponse, model string) *ChatResponse {
	var choices []Choice

	for i, candidate := range resp.Candidates {
		content := ""
		if len(candidate.Content.Parts) > 0 {
			content = candidate.Content.Parts[0].Text
		}

		choices = append(choices, Choice{
			Index: i,
			Message: Message{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: candidate.FinishReason,
		})
	}

	var usage Usage
	if resp.UsageMetadata != nil {
		usage = Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return &ChatResponse{
		Model:   model,
		Choices: choices,
		Usage:   usage,
	}
}

// convertToStreamChunk 将Gemini流式响应转换为OpenAI格式
func (c *GeminiClient) convertToStreamChunk(resp GeminiStreamResponse, model string) StreamChunk {
	var choices []Choice

	for i, candidate := range resp.Candidates {
		content := ""
		if len(candidate.Content.Parts) > 0 {
			content = candidate.Content.Parts[0].Text
		}

		choices = append(choices, Choice{
			Index: i,
			Delta: Delta{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: candidate.FinishReason,
		})
	}

	return StreamChunk{
		Model:   model,
		Choices: choices,
	}
}
