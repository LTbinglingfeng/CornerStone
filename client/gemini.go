package client

import (
	"context"
	"cornerstone/logging"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/genai"
)

// GeminiClient Google Gemini API 客户端
type GeminiClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	genaiBaseURL string
	genaiVersion string
}

// NewGeminiClient 创建新的Gemini客户端
func NewGeminiClient(baseURL, apiKey string) *GeminiClient {
	// 默认使用 Google Gemini API 地址
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	genaiBaseURL, genaiVersion := normalizeGeminiBaseURL(baseURL)
	return &GeminiClient{
		BaseURL:      strings.TrimSuffix(baseURL, "/"),
		APIKey:       apiKey,
		HTTPClient:   newHTTPClient(),
		genaiBaseURL: genaiBaseURL,
		genaiVersion: genaiVersion,
	}
}

func (c *GeminiClient) newGenAIClient(ctx context.Context) (*genai.Client, error) {
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

// convertToGenAIContents 将通用消息转换为 genai 格式
func (c *GeminiClient) convertToGenAIContents(messages []Message) ([]*genai.Content, *genai.Content, error) {
	contents := make([]*genai.Content, 0, len(messages))
	var systemInstruction *genai.Content

	for _, msg := range messages {
		if msg.Role == "system" {
			systemInstruction = &genai.Content{
				Parts: []*genai.Part{{Text: msg.Content}},
			}
			continue
		}

		if msg.Role == "tool" {
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				// Legacy/fallback: treat as user text if tool_call_id is missing.
				contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleUser))
				continue
			}

			toolName := ""
			var parsed struct {
				Tool string `json:"tool"`
			}
			if errUnmarshal := json.Unmarshal([]byte(msg.Content), &parsed); errUnmarshal == nil {
				toolName = strings.TrimSpace(parsed.Tool)
			}
			if toolName == "" {
				toolName = "unknown_tool"
			}

			response := make(map[string]any)
			if errUnmarshal := json.Unmarshal([]byte(msg.Content), &response); errUnmarshal != nil {
				response = map[string]any{
					"ok":    false,
					"tool":  toolName,
					"data":  nil,
					"error": "invalid tool result payload",
				}
			}

			part := genai.NewPartFromFunctionResponse(toolName, response)
			part.FunctionResponse.ID = callID
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{part}, genai.RoleUser))
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = string(genai.RoleModel)
		} else {
			role = string(genai.RoleUser)
		}

		parts := make([]*genai.Part, 0, len(msg.ImagePaths)+1+len(msg.ToolCalls))
		if strings.TrimSpace(msg.Content) != "" {
			parts = append(parts, genai.NewPartFromText(msg.Content))
		}
		for _, imagePath := range msg.ImagePaths {
			payload, errLoad := loadImagePayload(imagePath)
			if errLoad != nil {
				return nil, nil, errLoad
			}
			raw, errDecode := base64.StdEncoding.DecodeString(payload.Data)
			if errDecode != nil {
				return nil, nil, fmt.Errorf("decode image payload: %w", errDecode)
			}
			parts = append(parts, genai.NewPartFromBytes(raw, payload.MimeType))
		}

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				toolName := strings.TrimSpace(toolCall.Function.Name)
				if toolName == "" {
					continue
				}

				args := make(map[string]any)
				trimmed := strings.TrimSpace(toolCall.Function.Arguments)
				if trimmed != "" {
					_ = json.Unmarshal([]byte(trimmed), &args)
				}

				part := genai.NewPartFromFunctionCall(toolName, args)
				if strings.TrimSpace(toolCall.ID) != "" {
					part.FunctionCall.ID = strings.TrimSpace(toolCall.ID)
				}
				parts = append(parts, part)
			}
		}

		if len(parts) == 0 {
			parts = append(parts, genai.NewPartFromText(""))
		}

		contents = append(contents, &genai.Content{Role: role, Parts: parts})
	}

	return contents, systemInstruction, nil
}

// convertToGenAITools 将 OpenAI 格式的 Tools 转换为 genai.Tools
func (c *GeminiClient) convertToGenAITools(tools []Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	declarations := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		if tool.Type == "function" {
			declaration := &genai.FunctionDeclaration{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
			}
			if tool.Function.Parameters != nil {
				declaration.ParametersJsonSchema = tool.Function.Parameters
			}
			declarations = append(declarations, declaration)
		}
	}

	if len(declarations) == 0 {
		return nil
	}

	return []*genai.Tool{{FunctionDeclarations: declarations}}
}

// Chat 发送聊天请求（非流式）
func (c *GeminiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	contents, systemInstruction, errConvert := c.convertToGenAIContents(req.Messages)
	if errConvert != nil {
		return nil, fmt.Errorf("build request: %w", errConvert)
	}

	ctx, cancel := context.WithTimeout(ctx, chatRequestTimeout)
	defer cancel()

	genaiClient, errInit := c.newGenAIClient(ctx)
	if errInit != nil {
		return nil, fmt.Errorf("init genai client: %w", errInit)
	}

	generationConfig := buildGenAIGenerateContentConfig(req, systemInstruction, c.convertToGenAITools(req.Tools))
	resp, err := genaiClient.Models.GenerateContent(ctx, req.Model, contents, generationConfig)
	if err != nil {
		translated := translateGenAIError(err)
		logging.Errorf("gemini request failed: model=%s err=%v", req.Model, translated)
		return nil, translated
	}

	// 转换为通用响应格式
	return c.convertToOpenAIResponse(resp, req.Model), nil
}

// ChatStream 发送聊天请求（流式）
func (c *GeminiClient) ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error {
	contents, systemInstruction, errConvert := c.convertToGenAIContents(req.Messages)
	if errConvert != nil {
		return fmt.Errorf("build request: %w", errConvert)
	}

	ctx, cancel := context.WithTimeout(ctx, streamRequestTimeout)
	defer cancel()

	genaiClient, errInit := c.newGenAIClient(ctx)
	if errInit != nil {
		return fmt.Errorf("init genai client: %w", errInit)
	}

	nextToolCallIndex := 0
	generationConfig := buildGenAIGenerateContentConfig(req, systemInstruction, c.convertToGenAITools(req.Tools))
	for resp, err := range genaiClient.Models.GenerateContentStream(ctx, req.Model, contents, generationConfig) {
		if err != nil {
			translated := translateGenAIError(err)
			logging.Errorf("gemini stream request failed: model=%s err=%v", req.Model, translated)
			return translated
		}
		chunk := c.convertToStreamChunk(resp, req.Model, &nextToolCallIndex)
		if errCallback := callback(chunk); errCallback != nil {
			return errCallback
		}
	}
	return nil
}

// convertToOpenAIResponse 将Gemini响应转换为OpenAI格式
func (c *GeminiClient) convertToOpenAIResponse(resp *genai.GenerateContentResponse, model string) *ChatResponse {
	var choices []Choice

	if resp == nil {
		return &ChatResponse{Model: model}
	}

	for i, candidate := range resp.Candidates {
		if candidate == nil {
			continue
		}
		var content strings.Builder
		var reasoning strings.Builder
		var toolCalls []ToolCall

		if candidate.Content != nil {
			for j, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				if part.Text != "" {
					if part.Thought {
						reasoning.WriteString(part.Text)
					} else {
						content.WriteString(part.Text)
					}
				}
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					callID := strings.TrimSpace(part.FunctionCall.ID)
					if callID == "" {
						callID = fmt.Sprintf("call_%d_%d", i, j)
					}
					toolCalls = append(toolCalls, ToolCall{
						ID:   callID,
						Type: "function",
						Function: ToolCallFunction{
							Name:      part.FunctionCall.Name,
							Arguments: string(argsJSON),
						},
					})
				}
			}
		}

		choices = append(choices, Choice{
			Index: i,
			Message: Message{
				Role:             "assistant",
				Content:          content.String(),
				ReasoningContent: reasoning.String(),
				ToolCalls:        toolCalls,
			},
			FinishReason: string(candidate.FinishReason),
		})
	}

	var usage Usage
	if resp.UsageMetadata != nil {
		usage = Usage{
			PromptTokens:     int(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:      int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	return &ChatResponse{
		Model:   model,
		Choices: choices,
		Usage:   usage,
	}
}

// convertToStreamChunk 将Gemini流式响应转换为OpenAI格式
func (c *GeminiClient) convertToStreamChunk(resp *genai.GenerateContentResponse, model string, nextToolCallIndex *int) StreamChunk {
	var choices []Choice

	if resp == nil {
		return StreamChunk{Model: model}
	}

	for i, candidate := range resp.Candidates {
		if candidate == nil {
			continue
		}
		var content strings.Builder
		var reasoning strings.Builder
		var deltaToolCalls []DeltaToolCall

		if candidate.Content != nil {
			for j, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				if part.Text != "" {
					if part.Thought {
						reasoning.WriteString(part.Text)
					} else {
						content.WriteString(part.Text)
					}
				}
				if part.FunctionCall != nil {
					argsJSON, _ := json.Marshal(part.FunctionCall.Args)
					callIndex := j
					callID := strings.TrimSpace(part.FunctionCall.ID)
					if callID == "" {
						callID = fmt.Sprintf("call_%d_%d", i, j)
					}
					if nextToolCallIndex != nil {
						callIndex = *nextToolCallIndex
						if strings.TrimSpace(part.FunctionCall.ID) == "" {
							callID = fmt.Sprintf("call_%d", callIndex)
						}
						*nextToolCallIndex++
					}
					deltaToolCalls = append(deltaToolCalls, DeltaToolCall{
						Index: callIndex,
						ID:    callID,
						Type:  "function",
						Function: ToolCallFunction{
							Name:      part.FunctionCall.Name,
							Arguments: string(argsJSON),
						},
					})
				}
			}
		}

		choices = append(choices, Choice{
			Index: i,
			Delta: Delta{
				Role:             "assistant",
				Content:          content.String(),
				ReasoningContent: reasoning.String(),
				ToolCalls:        deltaToolCalls,
			},
			FinishReason: string(candidate.FinishReason),
		})
	}

	return StreamChunk{
		Model:   model,
		Choices: choices,
	}
}

type genAIThinkingBuild struct {
	ThinkingConfig *genai.ThinkingConfig
	ExtraBody      map[string]any
}

func buildGeminiThinkingConfig(req ChatRequest) genAIThinkingBuild {
	mode := strings.TrimSpace(req.GeminiThinkingMode)
	switch mode {
	case "none":
		return genAIThinkingBuild{}
	case "thinking_level":
		level := normalizeGeminiThinkingLevel(req.Model, req.GeminiThinkingLevel)
		return genAIThinkingBuild{
			ThinkingConfig: &genai.ThinkingConfig{
				IncludeThoughts: true,
			},
			ExtraBody: map[string]any{
				"generationConfig": map[string]any{
					"thinkingConfig": map[string]any{
						"thinkingLevel": level,
					},
				},
			},
		}
	case "thinking_budget":
		budget := req.GeminiThinkingBudget
		// -1 enables dynamic thinking (model adjusts budget based on complexity).
		if budget != -1 {
			minBudget, maxBudget := geminiThinkingBudgetRange(req.Model)
			if budget < minBudget {
				budget = minBudget
			}
			if budget > maxBudget {
				budget = maxBudget
			}
		}
		includeThoughts := budget != 0
		budget32 := int32(budget)
		return genAIThinkingBuild{
			ThinkingConfig: &genai.ThinkingConfig{
				IncludeThoughts: includeThoughts,
				ThinkingBudget:  &budget32,
			},
		}
	default:
		return genAIThinkingBuild{}
	}
}

func buildGenAIGenerateContentConfig(req ChatRequest, systemInstruction *genai.Content, tools []*genai.Tool) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: systemInstruction,
		Tools:             tools,
	}

	if req.Temperature > 0 {
		cfg.Temperature = genai.Ptr(float32(req.Temperature))
	}
	if req.TopP > 0 {
		cfg.TopP = genai.Ptr(float32(req.TopP))
	}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(req.MaxTokens)
	}

	thinking := buildGeminiThinkingConfig(req)
	if thinking.ThinkingConfig != nil {
		cfg.ThinkingConfig = thinking.ThinkingConfig
	}
	if thinking.ExtraBody != nil {
		cfg.HTTPOptions = &genai.HTTPOptions{ExtraBody: thinking.ExtraBody}
	}

	return cfg
}

func translateGenAIError(err error) error {
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
	return fmt.Errorf("generate content: %w", err)
}

func normalizeGeminiBaseURL(baseURL string) (cleanBaseURL, apiVersion string) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", ""
	}

	trimmed := strings.TrimSuffix(baseURL, "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed, ""
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if parsed.Path == "" || parsed.Path == "." {
		return parsed.String(), ""
	}

	segments := strings.Split(parsed.Path, "/")
	last := segments[len(segments)-1]
	if isGeminiAPIVersion(last) {
		apiVersion = last
		segments = segments[:len(segments)-1]
		parsed.Path = strings.Join(segments, "/")
	}

	return strings.TrimSuffix(parsed.String(), "/"), apiVersion
}

func isGeminiAPIVersion(segment string) bool {
	switch segment {
	case "v1", "v1beta", "v1beta1", "v1alpha", "v1alpha1":
		return true
	default:
		return false
	}
}

func normalizeGeminiThinkingLevel(model, level string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	level = strings.ToLower(strings.TrimSpace(level))

	// Gemini 3 Flash supports: minimal/low/medium/high.
	if strings.Contains(model, "gemini-3") && strings.Contains(model, "flash") {
		switch level {
		case "minimal", "low", "medium", "high":
			return level
		default:
			return "low"
		}
	}

	// Default: low/high.
	if level == "high" {
		return "high"
	}
	return "low"
}

func geminiThinkingBudgetRange(model string) (minBudget, maxBudget int) {
	model = strings.ToLower(strings.TrimSpace(model))

	// Defaults based on https://ai.google.dev/gemini-api/docs/thinking
	switch {
	case strings.Contains(model, "flash-lite"):
		return 512, 24576
	case strings.Contains(model, "flash"):
		return 0, 24576
	case strings.Contains(model, "pro"):
		return 128, 32768
	case strings.Contains(model, "robotics-er"):
		return 0, 24576
	default:
		return 128, 32768
	}
}
