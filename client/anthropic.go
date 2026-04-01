package client

import (
	"bufio"
	"bytes"
	"context"
	"cornerstone/logging"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultAnthropicMaxTokens = 1024

// AnthropicClient Anthropic Claude API 客户端
type AnthropicClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// AnthropicRequest Anthropic 消息请求
type AnthropicRequest struct {
	Model       string                  `json:"model"`
	Messages    []AnthropicMessage      `json:"messages"`
	System      []AnthropicContentBlock `json:"system,omitempty"`
	Stream      bool                    `json:"stream,omitempty"`
	MaxTokens   int                     `json:"max_tokens"`
	Temperature float64                 `json:"temperature,omitempty"`
	TopP        float64                 `json:"top_p,omitempty"`
	Thinking    *AnthropicThinking      `json:"thinking,omitempty"`
	Tools       []AnthropicTool         `json:"tools,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

type AnthropicContentBlock struct {
	Type      string                  `json:"type"`
	Text      string                  `json:"text,omitempty"`
	Signature string                  `json:"signature,omitempty"`
	Thinking  string                  `json:"thinking,omitempty"`
	Source    *AnthropicImageSource   `json:"source,omitempty"`
	ID        string                  `json:"id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Input     json.RawMessage         `json:"input,omitempty"`
	ToolUseID string                  `json:"tool_use_id,omitempty"`
	IsError   bool                    `json:"is_error,omitempty"`
	Content   []AnthropicContentBlock `json:"content,omitempty"`
}

type AnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []AnthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence"`
	Usage        *AnthropicUsage         `json:"usage,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicStreamEvent struct {
	Type         string                  `json:"type"`
	Index        int                     `json:"index,omitempty"`
	Message      *anthropicStreamMessage `json:"message,omitempty"`
	ContentBlock *AnthropicContentBlock  `json:"content_block,omitempty"`
	Delta        *anthropicStreamDelta   `json:"delta,omitempty"`
}

type anthropicStreamMessage struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Model string `json:"model"`
}

type anthropicStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
}

type anthropicToolUseState struct {
	ID   string
	Name string
}

// NewAnthropicClient 创建新的Anthropic客户端
func NewAnthropicClient(baseURL, apiKey string) *AnthropicClient {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicClient{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: newHTTPClient(),
	}
}

// Chat 发送聊天请求（非流式）
func (c *AnthropicClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	ctx, cancel := context.WithTimeout(ctx, chatRequestTimeout)
	defer cancel()

	anthropicReq, errBuild := buildAnthropicRequest(req)
	if errBuild != nil {
		return nil, fmt.Errorf("build request: %w", errBuild)
	}

	body, errMarshal := json.Marshal(anthropicReq)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal request: %w", errMarshal)
	}

	httpReq, errCreate := http.NewRequestWithContext(ctx, "POST", c.buildMessagesURL(), bytes.NewReader(body))
	if errCreate != nil {
		return nil, fmt.Errorf("create request: %w", errCreate)
	}

	c.setHeaders(httpReq)

	resp, errDo := c.HTTPClient.Do(httpReq)
	if errDo != nil {
		logging.Errorf("anthropic request failed: model=%s err=%v", req.Model, errDo)
		return nil, fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close anthropic response body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		logging.Errorf(
			"anthropic API error: model=%s status=%d body=%s",
			req.Model,
			resp.StatusCode,
			logging.Truncate(string(bodyBytes), 500),
		)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var anthropicResp AnthropicResponse
	if errDecode := json.NewDecoder(resp.Body).Decode(&anthropicResp); errDecode != nil {
		logging.Errorf("anthropic response decode failed: model=%s err=%v", req.Model, errDecode)
		return nil, fmt.Errorf("decode response: %w", errDecode)
	}

	return convertAnthropicResponse(anthropicResp, req.Model), nil
}

// ChatStream 发送聊天请求（流式）
func (c *AnthropicClient) ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error {
	req.Stream = true

	ctx, cancel := context.WithTimeout(ctx, streamRequestTimeout)
	defer cancel()

	anthropicReq, errBuild := buildAnthropicRequest(req)
	if errBuild != nil {
		return fmt.Errorf("build request: %w", errBuild)
	}

	body, errMarshal := json.Marshal(anthropicReq)
	if errMarshal != nil {
		return fmt.Errorf("marshal request: %w", errMarshal)
	}

	httpReq, errCreate := http.NewRequestWithContext(ctx, "POST", c.buildMessagesURL(), bytes.NewReader(body))
	if errCreate != nil {
		return fmt.Errorf("create request: %w", errCreate)
	}

	c.setHeaders(httpReq)

	resp, errDo := c.HTTPClient.Do(httpReq)
	if errDo != nil {
		logging.Errorf("anthropic stream request failed: model=%s err=%v", req.Model, errDo)
		return fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close anthropic response body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		logging.Errorf(
			"anthropic stream API error: model=%s status=%d body=%s",
			req.Model,
			resp.StatusCode,
			logging.Truncate(string(bodyBytes), 500),
		)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxStreamLineBytes)

	toolUses := make(map[int]anthropicToolUseState)
	streamModel := req.Model

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var event anthropicStreamEvent
		if errUnmarshal := json.Unmarshal([]byte(data), &event); errUnmarshal != nil {
			logging.Warnf("anthropic stream event unmarshal failed: model=%s data=%s err=%v", req.Model, logging.Truncate(data, 200), errUnmarshal)
			continue
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil && event.Message.Model != "" {
				streamModel = event.Message.Model
			}
			chunk := StreamChunk{
				Model: streamModel,
				Choices: []Choice{
					{
						Index: 0,
						Delta: Delta{
							Role: "assistant",
						},
					},
				},
			}
			if errCallback := callback(chunk); errCallback != nil {
				return errCallback
			}
		case "content_block_start":
			if event.ContentBlock == nil || event.ContentBlock.Type != "tool_use" {
				continue
			}
			toolID := event.ContentBlock.ID
			if toolID == "" {
				toolID = fmt.Sprintf("call_%d", event.Index)
			}
			toolName := event.ContentBlock.Name
			toolUses[event.Index] = anthropicToolUseState{ID: toolID, Name: toolName}

			chunk := StreamChunk{
				Model: streamModel,
				Choices: []Choice{
					{
						Index: 0,
						Delta: Delta{
							ToolCalls: []DeltaToolCall{
								{
									Index: event.Index,
									ID:    toolID,
									Type:  "function",
									Function: ToolCallFunction{
										Name: toolName,
									},
								},
							},
						},
					},
				},
			}
			if errCallback := callback(chunk); errCallback != nil {
				return errCallback
			}
		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text == "" {
					continue
				}
				chunk := StreamChunk{
					Model: streamModel,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{
								Content: event.Delta.Text,
							},
						},
					},
				}
				if errCallback := callback(chunk); errCallback != nil {
					return errCallback
				}
			case "thinking_delta":
				if event.Delta.Thinking == "" {
					continue
				}
				chunk := StreamChunk{
					Model: streamModel,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{
								ReasoningContent: event.Delta.Thinking,
							},
						},
					},
				}
				if errCallback := callback(chunk); errCallback != nil {
					return errCallback
				}
			case "input_json_delta":
				if event.Delta.PartialJSON == "" {
					continue
				}
				toolUse, ok := toolUses[event.Index]
				if !ok {
					continue
				}
				chunk := StreamChunk{
					Model: streamModel,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{
								ToolCalls: []DeltaToolCall{
									{
										Index: event.Index,
										ID:    toolUse.ID,
										Type:  "function",
										Function: ToolCallFunction{
											Name:      toolUse.Name,
											Arguments: event.Delta.PartialJSON,
										},
									},
								},
							},
						},
					},
				}
				if errCallback := callback(chunk); errCallback != nil {
					return errCallback
				}
			}
		case "message_delta":
			if event.Delta == nil || event.Delta.StopReason == "" {
				continue
			}
			chunk := StreamChunk{
				Model: streamModel,
				Choices: []Choice{
					{
						Index:        0,
						FinishReason: event.Delta.StopReason,
					},
				},
			}
			if errCallback := callback(chunk); errCallback != nil {
				return errCallback
			}
		}
	}

	return scanner.Err()
}

func (c *AnthropicClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

func (c *AnthropicClient) buildMessagesURL() string {
	baseURL := strings.TrimSuffix(c.BaseURL, "/")
	if strings.HasSuffix(baseURL, "/messages") {
		return baseURL
	}
	return baseURL + "/messages"
}

func buildAnthropicRequest(req ChatRequest) (AnthropicRequest, error) {
	messages, system, errBuild := buildAnthropicMessages(req.Messages)
	if errBuild != nil {
		return AnthropicRequest{}, errBuild
	}

	maxTokens := req.MaxTokens
	maxTokensProvided := maxTokens > 0
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	anthropicReq := AnthropicRequest{
		Model:     req.Model,
		Messages:  messages,
		System:    system,
		Stream:    req.Stream,
		MaxTokens: maxTokens,
		Tools:     convertToAnthropicTools(req.Tools),
	}
	if req.ThinkingBudget > 0 {
		thinkingBudget := req.ThinkingBudget
		if thinkingBudget < 1024 {
			thinkingBudget = 1024
		}
		if maxTokens <= thinkingBudget {
			if maxTokensProvided {
				maxTokens = thinkingBudget + 1
			} else {
				maxTokens = thinkingBudget + defaultAnthropicMaxTokens
			}
			anthropicReq.MaxTokens = maxTokens
		}
		anthropicReq.Thinking = &AnthropicThinking{
			Type:         "enabled",
			BudgetTokens: thinkingBudget,
		}
	}
	// Anthropic rejects temperature and top_p when both are provided.
	if req.Temperature > 0 {
		anthropicReq.Temperature = req.Temperature
	} else if req.TopP > 0 {
		anthropicReq.TopP = req.TopP
	}

	return anthropicReq, nil
}

func buildAnthropicMessages(messages []Message) ([]AnthropicMessage, []AnthropicContentBlock, error) {
	if len(messages) == 0 {
		return nil, nil, nil
	}

	anthropicMessages := make([]AnthropicMessage, 0, len(messages))
	systemParts := make([]string, 0, 1)

	for _, msg := range messages {
		if msg.Role == "system" {
			if strings.TrimSpace(msg.Content) != "" {
				systemParts = append(systemParts, msg.Content)
			}
			continue
		}

		if msg.Role == "tool" {
			if msg.ToolCallID == "" {
				continue
			}
			anthropicMessages = append(anthropicMessages, AnthropicMessage{
				Role: "user",
				Content: []AnthropicContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: msg.ToolCallID,
						Content: []AnthropicContentBlock{
							{
								Type: "text",
								Text: msg.Content,
							},
						},
					},
				},
			})
			continue
		}

		blocks, errBuild := buildAnthropicContentBlocks(msg)
		if errBuild != nil {
			return nil, nil, errBuild
		}

		role := msg.Role
		if role != "assistant" {
			role = "user"
		}

		anthropicMessages = append(anthropicMessages, AnthropicMessage{
			Role:    role,
			Content: blocks,
		})
	}

	systemText := strings.TrimSpace(strings.Join(systemParts, "\n\n"))
	if systemText == "" {
		return anthropicMessages, nil, nil
	}
	return anthropicMessages, []AnthropicContentBlock{
		{
			Type: "text",
			Text: systemText,
		},
	}, nil
}

func buildAnthropicContentBlocks(msg Message) ([]AnthropicContentBlock, error) {
	blocks := make([]AnthropicContentBlock, 0, len(msg.ImagePaths)+1+len(msg.ToolCalls))

	if strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, AnthropicContentBlock{
			Type: "text",
			Text: msg.Content,
		})
	}

	for _, imagePath := range msg.ImagePaths {
		payload, errLoad := loadImagePayload(imagePath)
		if errLoad != nil {
			return nil, errLoad
		}
		blocks = append(blocks, AnthropicContentBlock{
			Type: "image",
			Source: &AnthropicImageSource{
				Type:      "base64",
				MediaType: payload.MimeType,
				Data:      payload.Data,
			},
		})
	}

	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		for i, toolCall := range msg.ToolCalls {
			toolName := strings.TrimSpace(toolCall.Function.Name)
			if toolName == "" {
				continue
			}
			toolID := toolCall.ID
			if toolID == "" {
				toolID = fmt.Sprintf("call_%d", i)
			}

			inputJSON := bytes.TrimSpace([]byte(toolCall.Function.Arguments))
			if len(inputJSON) == 0 || !json.Valid(inputJSON) {
				inputJSON = []byte("{}")
			}

			blocks = append(blocks, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    toolID,
				Name:  toolName,
				Input: inputJSON,
			})
		}
	}

	if len(blocks) == 0 {
		blocks = append(blocks, AnthropicContentBlock{
			Type: "text",
			Text: "",
		})
	}

	return blocks, nil
}

func convertToAnthropicTools(tools []Tool) []AnthropicTool {
	if len(tools) == 0 {
		return nil
	}

	converted := make([]AnthropicTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		converted = append(converted, AnthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}

	if len(converted) == 0 {
		return nil
	}

	return converted
}

func convertAnthropicResponse(resp AnthropicResponse, fallbackModel string) *ChatResponse {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var toolCalls []ToolCall

	for i, block := range resp.Content {
		switch block.Type {
		case "text":
			contentBuilder.WriteString(block.Text)
		case "thinking":
			if block.Thinking != "" {
				reasoningBuilder.WriteString(block.Thinking)
			} else {
				reasoningBuilder.WriteString(block.Text)
			}
		case "tool_use":
			toolID := block.ID
			if toolID == "" {
				toolID = fmt.Sprintf("call_%d", i)
			}
			inputJSON := []byte("{}")
			if len(block.Input) > 0 {
				inputJSON = block.Input
				if !json.Valid(inputJSON) {
					inputJSON = []byte("{}")
				}
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   toolID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	model := resp.Model
	if model == "" {
		model = fallbackModel
	}

	message := Message{
		Role:      "assistant",
		Content:   contentBuilder.String(),
		ToolCalls: toolCalls,
	}
	if reasoningBuilder.Len() > 0 {
		message.ReasoningContent = reasoningBuilder.String()
	}

	var usage Usage
	if resp.Usage != nil {
		usage = Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return &ChatResponse{
		ID:     resp.ID,
		Object: resp.Type,
		Model:  model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      message,
				FinishReason: resp.StopReason,
			},
		},
		Usage: usage,
	}
}
