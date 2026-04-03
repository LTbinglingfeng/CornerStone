package client

import (
	"testing"

	"google.golang.org/genai"
)

func TestGeminiConvertToGenAIContents_EncodesToolCallsAndToolResults(t *testing.T) {
	c := NewGeminiClient("", "test")

	messages := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: ToolCallFunction{
						Name:      "send_pat",
						Arguments: `{"name":"a","target":"b"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"ok":true,"tool":"send_pat","data":{"name":"a","target":"b"},"error":""}`,
		},
		{Role: "assistant", Content: "done"},
	}

	contents, system, err := c.convertToGenAIContents(messages)
	if err != nil {
		t.Fatalf("convertToGenAIContents err: %v", err)
	}
	if system == nil || len(system.Parts) == 0 {
		t.Fatalf("expected system instruction")
	}
	if len(contents) != 4 {
		t.Fatalf("contents len=%d, want 4", len(contents))
	}

	toolCallContent := contents[1]
	if toolCallContent.Role != string(genai.RoleModel) {
		t.Fatalf("tool call role=%q, want %q", toolCallContent.Role, genai.RoleModel)
	}
	foundCall := false
	for _, part := range toolCallContent.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		if part.FunctionCall.Name != "send_pat" {
			t.Fatalf("function call name=%q, want send_pat", part.FunctionCall.Name)
		}
		if part.FunctionCall.ID != "call_1" {
			t.Fatalf("function call id=%q, want call_1", part.FunctionCall.ID)
		}
		foundCall = true
	}
	if !foundCall {
		t.Fatalf("expected functionCall part")
	}

	toolRespContent := contents[2]
	if toolRespContent.Role != string(genai.RoleUser) {
		t.Fatalf("tool response role=%q, want %q", toolRespContent.Role, genai.RoleUser)
	}
	foundResp := false
	for _, part := range toolRespContent.Parts {
		if part == nil || part.FunctionResponse == nil {
			continue
		}
		if part.FunctionResponse.Name != "send_pat" {
			t.Fatalf("function response name=%q, want send_pat", part.FunctionResponse.Name)
		}
		if part.FunctionResponse.ID != "call_1" {
			t.Fatalf("function response id=%q, want call_1", part.FunctionResponse.ID)
		}
		if ok, _ := part.FunctionResponse.Response["ok"].(bool); !ok {
			t.Fatalf("expected function response ok=true, got %v", part.FunctionResponse.Response["ok"])
		}
		foundResp = true
	}
	if !foundResp {
		t.Fatalf("expected functionResponse part")
	}
}
