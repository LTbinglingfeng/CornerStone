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
