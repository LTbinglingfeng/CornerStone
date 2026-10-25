package client

import (
	"encoding/json"
	"testing"
)

// Reasoning-only assistant turns replayed under a different model must not
// produce empty parts, which Gemini rejects ("required oneof field 'data'").
func TestGeminiConvertSkipsEmptyPartsOnModelMismatch(t *testing.T) {
	c := NewGeminiClient("", "test")
	payload, _ := json.Marshal([]map[string]any{{"text": "thinking", "thought": true}})
	messages := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", ReasoningContent: "thinking", ProviderState: &ProviderState{Provider: "gemini", Model: "model-a", Payload: payload}},
		{Role: "user", Content: "again"},
		{Role: "tool", Content: ""},
	}

	contents, _, err := c.convertToGenAIContentsForModel(messages, "model-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}
	for i, content := range contents {
		for j, part := range content.Parts {
			raw, _ := json.Marshal(part)
			if string(raw) == "{}" {
				t.Fatalf("contents[%d].parts[%d] is empty", i, j)
			}
		}
	}

	// Same model still replays the original parts.
	contents, _, err = c.convertToGenAIContentsForModel(messages, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 3 || contents[1].Role != "model" || !contents[1].Parts[0].Thought {
		t.Fatalf("expected replayed model turn, got %+v", contents)
	}
}
