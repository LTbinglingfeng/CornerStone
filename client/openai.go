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

// Message 表示对话消息
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // 思考模型的推理内容
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`        // 工具调用
	ToolCallID       string     `json:"tool_call_id,omitempty"`      // 工具调用ID (用于tool角色)
	ImagePaths       []string   `json:"image_paths,omitempty"`       // 图片路径
}

// Tool 工具定义
type Tool struct {
	Type     string       `json:"type"` // 目前只支持 "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction 工具函数定义
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 工具调用的函数信息
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON字符串
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model                string    `json:"model"`
	Messages             []Message `json:"messages"`
	Stream               bool      `json:"stream,omitempty"`
	Temperature          float64   `json:"temperature,omitempty"`
	TopP                 float64   `json:"top_p,omitempty"`
	MaxTokens            int       `json:"max_tokens,omitempty"`
	ThinkingBudget       int       `json:"thinking_budget,omitempty"`        // Anthropic思考预算
	ReasoningEffort      string    `json:"reasoning_effort,omitempty"`       // OpenAI兼容思考强度
	GeminiThinkingMode   string    `json:"gemini_thinking_mode,omitempty"`   // Gemini思考模式
	GeminiThinkingLevel  string    `json:"gemini_thinking_level,omitempty"`  // Gemini思考级别
	GeminiThinkingBudget int       `json:"gemini_thinking_budget,omitempty"` // Gemini思考预算
	Tools                []Tool    `json:"tools,omitempty"`                  // 工具列表
}

type OpenAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
}

type OpenAIImageURL struct {
	URL string `json:"url"`
}

type OpenAIMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type OpenAIChatRequest struct {
	Model           string          `json:"model"`
	Messages        []OpenAIMessage `json:"messages"`
	Stream          bool            `json:"stream,omitempty"`
	Temperature     float64         `json:"temperature,omitempty"`
	TopP            float64         `json:"top_p,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Tools           []Tool          `json:"tools,omitempty"` // 工具列表
}

// ChatResponse 非流式响应
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 响应选项
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	Delta        Delta   `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason"`
}

// Delta 流式响应增量
type Delta struct {
	Role             string          `json:"role,omitempty"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"` // 思考模型的推理内容
	ToolCalls        []DeltaToolCall `json:"tool_calls,omitempty"`        // 流式工具调用
}

// DeltaToolCall 流式工具调用增量
type DeltaToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function,omitempty"`
}

// Usage token使用情况
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk 流式响应块
type StreamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// OpenAIClient OpenAI兼容客户端
type OpenAIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient 创建新客户端
func NewClient(baseURL, apiKey string) *OpenAIClient {
	return &OpenAIClient{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: newHTTPClient(),
	}
}

// Chat 发送聊天请求（非流式）
func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	ctx, cancel := context.WithTimeout(ctx, chatRequestTimeout)
	defer cancel()

	openAIReq, errBuild := buildOpenAIRequest(req)
	if errBuild != nil {
		return nil, fmt.Errorf("build request: %w", errBuild)
	}

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &chatResp, nil
}

// ChatStream 发送聊天请求（流式）
func (c *OpenAIClient) ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error {
	req.Stream = true

	ctx, cancel := context.WithTimeout(ctx, streamRequestTimeout)
	defer cancel()

	openAIReq, errBuild := buildOpenAIRequest(req)
	if errBuild != nil {
		return fmt.Errorf("build request: %w", errBuild)
	}

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setHeaders(httpReq)

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

		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if err := callback(chunk); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func (c *OpenAIClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
}

func buildOpenAIRequest(req ChatRequest) (OpenAIChatRequest, error) {
	messages := make([]OpenAIMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content, errContent := buildOpenAIMessageContent(msg)
		if errContent != nil {
			return OpenAIChatRequest{}, errContent
		}
		messages = append(messages, OpenAIMessage{
			Role:       msg.Role,
			Content:    content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		})
	}

	return OpenAIChatRequest{
		Model:           req.Model,
		Messages:        messages,
		Stream:          req.Stream,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: req.ReasoningEffort,
		Tools:           req.Tools,
	}, nil
}

func buildOpenAIMessageContent(msg Message) (interface{}, error) {
	if len(msg.ImagePaths) == 0 {
		return msg.Content, nil
	}

	parts := make([]OpenAIContentPart, 0, len(msg.ImagePaths)+1)
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, OpenAIContentPart{
			Type: "text",
			Text: msg.Content,
		})
	}
	for _, imagePath := range msg.ImagePaths {
		payload, errLoad := loadImagePayload(imagePath)
		if errLoad != nil {
			return nil, errLoad
		}
		parts = append(parts, OpenAIContentPart{
			Type: "image_url",
			ImageURL: &OpenAIImageURL{
				URL: fmt.Sprintf("data:%s;base64,%s", payload.MimeType, payload.Data),
			},
		})
	}

	return parts, nil
}
