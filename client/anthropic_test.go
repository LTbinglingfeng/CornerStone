package client

import "testing"

func TestBuildAnthropicRequestPrefersTemperatureOverTopP(t *testing.T) {
	req, err := buildAnthropicRequest(ChatRequest{
		Model: "claude-sonnet",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Temperature: 1,
		TopP:        1,
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequest err = %v", err)
	}
	if req.Temperature != 1 {
		t.Fatalf("buildAnthropicRequest temperature = %v, want 1", req.Temperature)
	}
	if req.TopP != 0 {
		t.Fatalf("buildAnthropicRequest top_p = %v, want 0", req.TopP)
	}
}

func TestBuildAnthropicRequestUsesTopPWhenTemperatureMissing(t *testing.T) {
	req, err := buildAnthropicRequest(ChatRequest{
		Model: "claude-sonnet",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		TopP: 0.9,
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequest err = %v", err)
	}
	if req.Temperature != 0 {
		t.Fatalf("buildAnthropicRequest temperature = %v, want 0", req.Temperature)
	}
	if req.TopP != 0.9 {
		t.Fatalf("buildAnthropicRequest top_p = %v, want 0.9", req.TopP)
	}
}

func TestBuildAnthropicRequestAddsPromptCachingBreakpoints(t *testing.T) {
	req, err := buildAnthropicRequest(ChatRequest{
		Model: "claude-sonnet",
		Messages: []Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "hello"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:       "lookup_weather",
					Parameters: map[string]interface{}{"type": "object"},
				},
			},
		},
		PromptCaching:  true,
		PromptCacheTTL: "1h",
	})
	if err != nil {
		t.Fatalf("buildAnthropicRequest err = %v", err)
	}
	if req.CacheControl != nil {
		t.Fatalf("buildAnthropicRequest cache_control = %#v, want nil when explicit breakpoints exist", req.CacheControl)
	}
	if len(req.System) != 1 || req.System[0].CacheControl == nil {
		t.Fatalf("system cache_control not applied: %#v", req.System)
	}
	if req.System[0].CacheControl.Type != "ephemeral" || req.System[0].CacheControl.TTL != "1h" {
		t.Fatalf("system cache_control = %#v, want ephemeral/1h", req.System[0].CacheControl)
	}
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil {
		t.Fatalf("tool cache_control not applied: %#v", req.Tools)
	}
	if req.Tools[0].CacheControl.Type != "ephemeral" || req.Tools[0].CacheControl.TTL != "1h" {
		t.Fatalf("tool cache_control = %#v, want ephemeral/1h", req.Tools[0].CacheControl)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0].CacheControl == nil {
		t.Fatalf("message cache_control not applied: %#v", req.Messages)
	}
	if req.Messages[0].Content[0].CacheControl.Type != "ephemeral" || req.Messages[0].Content[0].CacheControl.TTL != "1h" {
		t.Fatalf("message cache_control = %#v, want ephemeral/1h", req.Messages[0].Content[0].CacheControl)
	}
}

func TestConvertAnthropicResponseIncludesCacheUsage(t *testing.T) {
	resp := convertAnthropicResponse(AnthropicResponse{
		ID:    "msg_123",
		Type:  "message",
		Model: "claude-sonnet",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "hello"},
		},
		Usage: &AnthropicUsage{
			InputTokens:              120,
			OutputTokens:             30,
			CacheCreationInputTokens: 90,
			CacheReadInputTokens:     60,
		},
	}, "fallback-model")

	if resp.Usage.PromptTokens != 120 {
		t.Fatalf("PromptTokens = %d, want 120", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 30 {
		t.Fatalf("CompletionTokens = %d, want 30", resp.Usage.CompletionTokens)
	}
	if resp.Usage.CacheCreationInputTokens != 90 {
		t.Fatalf("CacheCreationInputTokens = %d, want 90", resp.Usage.CacheCreationInputTokens)
	}
	if resp.Usage.CacheReadInputTokens != 60 {
		t.Fatalf("CacheReadInputTokens = %d, want 60", resp.Usage.CacheReadInputTokens)
	}
}
