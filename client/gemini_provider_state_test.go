package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func signedGeminiResponse() *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
		{Text: "reason", Thought: true, ThoughtSignature: []byte("reason-signature")},
		{FunctionCall: &genai.FunctionCall{Name: "search", Args: map[string]any{"q": "test"}}, ThoughtSignature: []byte("call-signature")},
		{Text: "answer", ThoughtSignature: []byte("text-signature")},
	}}}}}
}

func TestGeminiProviderStateRoundTrip(t *testing.T) {
	c := NewGeminiClient("", "test")
	msg := c.convertToOpenAIResponse(signedGeminiResponse(), "model-a").Choices[0].Message
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	contents, _, err := c.convertToGenAIContentsForModel([]Message{restored}, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	parts := contents[0].Parts
	if len(parts) != 3 || string(parts[1].ThoughtSignature) != "call-signature" || parts[1].FunctionCall.ID != msg.ToolCalls[0].ID || !parts[0].Thought {
		t.Fatalf("lost original parts: %+v", parts)
	}
	for _, other := range []struct{ provider, model string }{{"gemini", "model-b"}, {"anthropic", "model-a"}, {"openai_responses", "model-a"}} {
		copy := CloneMessage(restored)
		copy.ProviderState.Provider = other.provider
		got, _, err := c.convertToGenAIContentsForModel([]Message{copy}, other.model)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range got[0].Parts {
			if len(part.ThoughtSignature) != 0 {
				t.Fatal("cross-provider/model signature leak")
			}
		}
	}
	wire, err := buildOpenAIRequest(ChatRequest{Messages: []Message{restored}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(wire)
	if strings.Contains(string(encoded), "provider_state") || strings.Contains(string(encoded), "signature") {
		t.Fatalf("metadata leaked: %s", encoded)
	}
	clone := CloneMessage(restored)
	clone.ProviderState.Payload[0] = 'x'
	if reflect.DeepEqual(clone.ProviderState, restored.ProviderState) {
		t.Fatal("payload aliased")
	}
}

func TestGeminiStreamProviderStateSnapshots(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"answer\"}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"thoughtSignature\":\"c2ln\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	defer server.Close()
	c := NewGeminiClient(server.URL, "key")
	var states []*ProviderState
	err := c.ChatStream(context.Background(), ChatRequest{Model: "model-a", Messages: []Message{{Role: "user", Content: "hello"}}}, func(chunk StreamChunk) error {
		for _, choice := range chunk.Choices {
			states = append(states, choice.Delta.ProviderState)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d states", len(states))
	}
	var first, last []*genai.Part
	if err := json.Unmarshal(states[0].Payload, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(states[1].Payload, &last); err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(last) != 2 || string(last[1].ThoughtSignature) != "sig" {
		t.Fatal("stream lost signature or mutated prior snapshot")
	}
}
