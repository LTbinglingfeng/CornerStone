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

type openAIResponsesRequest struct {
	Model           string                    `json:"model"`
	Input           interface{}               `json:"input"`
	Stream          bool                      `json:"stream,omitempty"`
	Temperature     float64                   `json:"temperature,omitempty"`
	TopP            float64                   `json:"top_p,omitempty"`
	MaxOutputTokens int                       `json:"max_output_tokens,omitempty"`
	Reasoning       *openAIResponsesReasoning `json:"reasoning,omitempty"`
	Text            *openAIResponsesTextParam `json:"text,omitempty"`
	Tools           []openAIResponsesTool     `json:"tools,omitempty"`
}

type openAIResponsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type openAIResponsesTextParam struct {
	Format openAIResponsesTextFormat `json:"format"`
}

type openAIResponsesTextFormat struct {
	Type string `json:"type"`
}

type openAIResponsesTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
	Strict      bool                   `json:"strict"`
}

type openAIResponsesMessageItem struct {
	Type    string      `json:"type"`
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type openAIResponsesInputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type openAIResponsesFunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponsesFunctionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type openAIResponsesResponse struct {
	ID        string                      `json:"id"`
	Object    string                      `json:"object"`
	CreatedAt int64                       `json:"created_at"`
	Model     string                      `json:"model"`
	Output    []openAIResponsesOutputItem `json:"output"`
	Usage     openAIResponsesUsage        `json:"usage"`
	Error     *openAIResponsesError       `json:"error"`
}

type openAIResponsesError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type openAIResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openAIResponsesOutputItem struct {
	Type      string                      `json:"type"`
	Role      string                      `json:"role,omitempty"`
	Content   []openAIResponsesOutputPart `json:"content,omitempty"`
	CallID    string                      `json:"call_id,omitempty"`
	Name      string                      `json:"name,omitempty"`
	Arguments string                      `json:"arguments,omitempty"`
}

type openAIResponsesOutputPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openAIResponsesStreamEvent struct {
	Type string `json:"type"`
}

type openAIResponsesStreamErrorEvent struct {
	Type    string  `json:"type"`
	Code    *string `json:"code"`
	Message string  `json:"message"`
}

type openAIResponsesStreamOutputItemEvent struct {
	Type        string          `json:"type"`
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
}

type openAIResponsesStreamFunctionArgsDeltaEvent struct {
	Type        string `json:"type"`
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

type openAIResponsesStreamFunctionArgsDoneEvent struct {
	Type        string `json:"type"`
	ItemID      string `json:"item_id"`
	Name        string `json:"name"`
	OutputIndex int    `json:"output_index"`
	Arguments   string `json:"arguments"`
}

type openAIResponsesStreamTextDeltaEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

type openAIResponsesToolCallState struct {
	CallID             string
	Name               string
	SeenArgumentsDelta bool
}

// OpenAIResponsesClient OpenAI Responses API 客户端（/v1/responses）
type OpenAIResponsesClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewResponsesClient(baseURL, apiKey string) *OpenAIResponsesClient {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIResponsesClient{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: newHTTPClient(),
	}
}

func (c *OpenAIResponsesClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	ctx, cancel := context.WithTimeout(ctx, chatRequestTimeout)
	defer cancel()

	openAIReq, errBuild := buildOpenAIResponsesRequest(req)
	if errBuild != nil {
		return nil, fmt.Errorf("build request: %w", errBuild)
	}

	body, errMarshal := json.Marshal(openAIReq)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal request: %w", errMarshal)
	}

	httpReq, errCreate := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/responses", bytes.NewReader(body))
	if errCreate != nil {
		return nil, fmt.Errorf("create request: %w", errCreate)
	}
	c.setHeaders(httpReq)

	resp, errDo := c.HTTPClient.Do(httpReq)
	if errDo != nil {
		return nil, fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close openai responses body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var rawResp openAIResponsesResponse
	if errDecode := json.NewDecoder(resp.Body).Decode(&rawResp); errDecode != nil {
		return nil, fmt.Errorf("decode response: %w", errDecode)
	}
	if rawResp.Error != nil {
		return nil, fmt.Errorf("API error (%s): %s", rawResp.Error.Code, rawResp.Error.Message)
	}

	return convertResponsesToChatResponse(rawResp, req.Model), nil
}

func (c *OpenAIResponsesClient) ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error {
	req.Stream = true

	ctx, cancel := context.WithTimeout(ctx, streamRequestTimeout)
	defer cancel()

	openAIReq, errBuild := buildOpenAIResponsesRequest(req)
	if errBuild != nil {
		return fmt.Errorf("build request: %w", errBuild)
	}
	openAIReq.Stream = true

	body, errMarshal := json.Marshal(openAIReq)
	if errMarshal != nil {
		return fmt.Errorf("marshal request: %w", errMarshal)
	}

	httpReq, errCreate := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/responses", bytes.NewReader(body))
	if errCreate != nil {
		return fmt.Errorf("create request: %w", errCreate)
	}
	c.setHeaders(httpReq)

	resp, errDo := c.HTTPClient.Do(httpReq)
	if errDo != nil {
		return fmt.Errorf("do request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			logging.Warnf("close openai responses stream body error: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxStreamLineBytes)

	toolCalls := make(map[int]*openAIResponsesToolCallState)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var baseEvent openAIResponsesStreamEvent
		if errUnmarshal := json.Unmarshal([]byte(data), &baseEvent); errUnmarshal != nil {
			continue
		}

		switch baseEvent.Type {
		case "response.output_text.delta":
			var ev openAIResponsesStreamTextDeltaEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				continue
			}
			chunk := StreamChunk{
				Model: req.Model,
				Choices: []Choice{
					{
						Index: 0,
						Delta: Delta{
							Content: ev.Delta,
						},
					},
				},
			}
			if errCallback := callback(chunk); errCallback != nil {
				return errCallback
			}

		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			var ev openAIResponsesStreamTextDeltaEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				continue
			}
			chunk := StreamChunk{
				Model: req.Model,
				Choices: []Choice{
					{
						Index: 0,
						Delta: Delta{
							ReasoningContent: ev.Delta,
						},
					},
				},
			}
			if errCallback := callback(chunk); errCallback != nil {
				return errCallback
			}

		case "response.output_item.added":
			var ev openAIResponsesStreamOutputItemEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				continue
			}
			if errHandle := handleResponsesOutputItemAdded(ev, toolCalls, req.Model, callback); errHandle != nil {
				return errHandle
			}

		case "response.function_call_arguments.delta":
			var ev openAIResponsesStreamFunctionArgsDeltaEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				continue
			}
			state, ok := toolCalls[ev.OutputIndex]
			if !ok {
				state = &openAIResponsesToolCallState{}
				toolCalls[ev.OutputIndex] = state
			}
			state.SeenArgumentsDelta = true

			chunk := StreamChunk{
				Model: req.Model,
				Choices: []Choice{
					{
						Index: 0,
						Delta: Delta{
							ToolCalls: []DeltaToolCall{
								{
									Index: ev.OutputIndex,
									ID:    state.CallID,
									Type:  "function",
									Function: ToolCallFunction{
										Name:      state.Name,
										Arguments: ev.Delta,
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

		case "response.function_call_arguments.done":
			var ev openAIResponsesStreamFunctionArgsDoneEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				continue
			}

			state, ok := toolCalls[ev.OutputIndex]
			if !ok {
				state = &openAIResponsesToolCallState{}
				toolCalls[ev.OutputIndex] = state
			}
			if state.Name == "" && ev.Name != "" {
				state.Name = ev.Name
				chunk := StreamChunk{
					Model: req.Model,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{
								ToolCalls: []DeltaToolCall{
									{
										Index: ev.OutputIndex,
										ID:    state.CallID,
										Type:  "function",
										Function: ToolCallFunction{
											Name: ev.Name,
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

			if !state.SeenArgumentsDelta && ev.Arguments != "" {
				chunk := StreamChunk{
					Model: req.Model,
					Choices: []Choice{
						{
							Index: 0,
							Delta: Delta{
								ToolCalls: []DeltaToolCall{
									{
										Index: ev.OutputIndex,
										ID:    state.CallID,
										Type:  "function",
										Function: ToolCallFunction{
											Name:      state.Name,
											Arguments: ev.Arguments,
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

		case "error":
			var ev openAIResponsesStreamErrorEvent
			if errUnmarshal := json.Unmarshal([]byte(data), &ev); errUnmarshal != nil {
				return fmt.Errorf("stream error")
			}
			code := ""
			if ev.Code != nil {
				code = *ev.Code
			}
			if code == "" {
				return fmt.Errorf("stream error: %s", ev.Message)
			}
			return fmt.Errorf("stream error (%s): %s", code, ev.Message)
		}
	}

	return scanner.Err()
}

func handleResponsesOutputItemAdded(ev openAIResponsesStreamOutputItemEvent, toolCalls map[int]*openAIResponsesToolCallState, model string, callback func(chunk StreamChunk) error) error {
	var itemType struct {
		Type string `json:"type"`
	}
	if errUnmarshal := json.Unmarshal(ev.Item, &itemType); errUnmarshal != nil {
		return nil
	}
	if itemType.Type != "function_call" {
		return nil
	}

	var call openAIResponsesOutputItem
	if errUnmarshal := json.Unmarshal(ev.Item, &call); errUnmarshal != nil {
		return nil
	}

	state := toolCalls[ev.OutputIndex]
	if state == nil {
		state = &openAIResponsesToolCallState{}
		toolCalls[ev.OutputIndex] = state
	}
	if state.CallID == "" {
		state.CallID = call.CallID
	}
	if state.Name == "" {
		state.Name = call.Name
	}

	chunk := StreamChunk{
		Model: model,
		Choices: []Choice{
			{
				Index: 0,
				Delta: Delta{
					ToolCalls: []DeltaToolCall{
						{
							Index: ev.OutputIndex,
							ID:    state.CallID,
							Type:  "function",
							Function: ToolCallFunction{
								Name: state.Name,
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
	return nil
}

func (c *OpenAIResponsesClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
}

func buildOpenAIResponsesRequest(req ChatRequest) (openAIResponsesRequest, error) {
	if req.Model == "" {
		return openAIResponsesRequest{}, fmt.Errorf("missing model")
	}
	if len(req.Messages) == 0 {
		return openAIResponsesRequest{}, fmt.Errorf("missing messages")
	}

	inputItems := make([]interface{}, 0, len(req.Messages))
	for i, msg := range req.Messages {
		switch msg.Role {
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				return openAIResponsesRequest{}, fmt.Errorf("tool message missing tool_call_id")
			}
			inputItems = append(inputItems, openAIResponsesFunctionCallOutputItem{
				Type:   "function_call_output",
				CallID: callID,
				Output: msg.Content,
			})
			continue
		default:
			content, errContent := buildOpenAIResponsesMessageContent(msg)
			if errContent != nil {
				return openAIResponsesRequest{}, errContent
			}
			inputItems = append(inputItems, openAIResponsesMessageItem{
				Type:    "message",
				Role:    msg.Role,
				Content: content,
			})
		}

		for j, toolCall := range msg.ToolCalls {
			callID := strings.TrimSpace(toolCall.ID)
			if callID == "" {
				callID = fmt.Sprintf("call_%d_%d", i, j)
			}
			inputItems = append(inputItems, openAIResponsesFunctionCallItem{
				Type:      "function_call",
				CallID:    callID,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			})
		}
	}

	convertedTools := convertToOpenAIResponsesTools(req.Tools)

	openAIReq := openAIResponsesRequest{
		Model:           req.Model,
		Input:           inputItems,
		Stream:          req.Stream,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		MaxOutputTokens: req.MaxTokens,
		Text: &openAIResponsesTextParam{
			Format: openAIResponsesTextFormat{
				Type: "text",
			},
		},
		Tools: convertedTools,
	}
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		openAIReq.Reasoning = &openAIResponsesReasoning{Effort: req.ReasoningEffort}
	}
	return openAIReq, nil
}

func buildOpenAIResponsesMessageContent(msg Message) (interface{}, error) {
	if len(msg.ImagePaths) == 0 {
		return msg.Content, nil
	}

	parts := make([]openAIResponsesInputContentPart, 0, len(msg.ImagePaths)+1)
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, openAIResponsesInputContentPart{
			Type: "input_text",
			Text: msg.Content,
		})
	}
	for _, imagePath := range msg.ImagePaths {
		payload, errLoad := loadImagePayload(imagePath)
		if errLoad != nil {
			return nil, errLoad
		}
		parts = append(parts, openAIResponsesInputContentPart{
			Type:     "input_image",
			ImageURL: fmt.Sprintf("data:%s;base64,%s", payload.MimeType, payload.Data),
			Detail:   "auto",
		})
	}

	return parts, nil
}

func convertToOpenAIResponsesTools(tools []Tool) []openAIResponsesTool {
	if len(tools) == 0 {
		return nil
	}

	converted := make([]openAIResponsesTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		params := tool.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		converted = append(converted, openAIResponsesTool{
			Type:        "function",
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  params,
			Strict:      true,
		})
	}
	return converted
}

func convertResponsesToChatResponse(resp openAIResponsesResponse, fallbackModel string) *ChatResponse {
	model := resp.Model
	if model == "" {
		model = fallbackModel
	}

	var content strings.Builder
	var reasoning strings.Builder
	toolCalls := make([]ToolCall, 0)

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			if item.Role != "assistant" {
				continue
			}
			for _, part := range item.Content {
				if part.Type == "output_text" {
					content.WriteString(part.Text)
				}
			}
		case "reasoning":
			for _, part := range item.Content {
				if part.Type == "reasoning_text" || part.Type == "summary_text" {
					reasoning.WriteString(part.Text)
				}
			}
		case "function_call":
			if item.Name == "" {
				continue
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}

	message := Message{
		Role:             "assistant",
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
		ToolCalls:        toolCalls,
	}

	return &ChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.CreatedAt,
		Model:   model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      message,
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}
