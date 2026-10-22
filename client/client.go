package client

import "context"

// AIClient 通用AI客户端接口
type AIClient interface {
	// Chat 发送聊天请求（非流式）
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 发送聊天请求（流式）
	ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error
}

// ChatRequest 通用聊天请求。
type ChatRequest struct {
	Model                       string    `json:"model"`
	Messages                    []Message `json:"messages"`
	Stream                      bool      `json:"stream,omitempty"`
	Temperature                 *float64  `json:"temperature,omitempty"` // nil 表示不向上游发送
	TopP                        *float64  `json:"top_p,omitempty"`       // nil 表示不向上游发送
	MaxTokens                   int       `json:"max_tokens,omitempty"`
	ThinkingBudget              int       `json:"thinking_budget,omitempty"`               // Anthropic思考预算
	PromptCaching               bool      `json:"prompt_caching,omitempty"`                // Anthropic Prompt Caching
	PromptCacheTTL              string    `json:"prompt_cache_ttl,omitempty"`              // Anthropic Prompt Cache TTL (5m/1h)
	ReasoningEffort             string    `json:"reasoning_effort,omitempty"`              // OpenAI兼容思考强度
	GeminiAutomaticContinuation bool      `json:"gemini_automatic_continuation,omitempty"` // Gemini 自动续传
	GeminiThinkingMode          string    `json:"gemini_thinking_mode,omitempty"`          // Gemini思考模式
	GeminiThinkingLevel         string    `json:"gemini_thinking_level,omitempty"`         // Gemini思考级别
	GeminiThinkingBudget        int       `json:"gemini_thinking_budget,omitempty"`        // Gemini思考预算
	Tools                       []Tool    `json:"tools,omitempty"`                         // 工具列表
}
