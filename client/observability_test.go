package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cornerstone/logging"
)

type observationFake struct {
	response *ChatResponse
	chunks   []StreamChunk
	err      error
}

func (f observationFake) Chat(context.Context, ChatRequest) (*ChatResponse, error) {
	return f.response, f.err
}
func (f observationFake) ChatStream(_ context.Context, _ ChatRequest, cb func(StreamChunk) error) error {
	for _, chunk := range f.chunks {
		if err := cb(chunk); err != nil {
			return err
		}
	}
	return f.err
}

func TestUsagePresence(t *testing.T) {
	for _, tc := range []struct {
		data    string
		present bool
	}{
		{`{}`, false}, {`{"usage":null}`, false}, {`{"usage":{}}`, false}, {`{"usage":{"prompt_tokens":null}}`, false},
		{`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`, true},
	} {
		var r ChatResponse
		if err := json.Unmarshal([]byte(tc.data), &r); err != nil {
			t.Fatal(err)
		}
		if r.Usage.Present != tc.present {
			t.Fatalf("%s: %+v", tc.data, r.Usage)
		}
		var raw openAIResponsesResponse
		responsesData := strings.ReplaceAll(strings.ReplaceAll(tc.data, "prompt_tokens", "input_tokens"), "completion_tokens", "output_tokens")
		if err := json.Unmarshal([]byte(responsesData), &raw); err != nil {
			t.Fatal(err)
		}
		if responsesUsage(raw.Usage).Present != tc.present {
			t.Fatal("responses presence")
		}
	}
	b, _ := json.Marshal(Usage{Present: true})
	if strings.Contains(string(b), "Present") || strings.Contains(string(b), "present") {
		t.Fatal(string(b))
	}
	if anthropicUsage(nil).Present || geminiUsage(nil).Present {
		t.Fatal("missing usage reported")
	}
	if !anthropicUsage(&AnthropicUsage{}).Present {
		t.Fatal("explicit zero lost")
	}
}

func TestObservedSnapshotAndFailure(t *testing.T) {
	sentinel := errors.New("private upstream failure")
	c := Observe(observationFake{chunks: []StreamChunk{
		{Usage: &Usage{Present: true, PromptTokens: 4, CompletionTokens: 1, TotalTokens: 5}},
		{Usage: &Usage{Present: true, PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7}},
	}, err: sentinel})
	if err := c.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) error { return nil }); err != sentinel {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.ModelCalls != 1 || s.UsageCalls != 1 || s.Usage.TotalTokens != 7 {
		t.Fatalf("%+v", s)
	}
	c.upstream = observationFake{response: &ChatResponse{Usage: Usage{Present: true}}}
	c.Chat(context.Background(), ChatRequest{})
	c.upstream = observationFake{err: sentinel}
	c.Chat(context.Background(), ChatRequest{})
	s = c.Snapshot()
	if s.ModelCalls != 3 || s.UsageCalls != 2 || s.Usage.TotalTokens != 7 {
		t.Fatalf("%+v", s)
	}
	// Snapshot is a copy and concurrent completion is safe.
	s.Usage.TotalTokens = 900
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { c.Chat(context.Background(), ChatRequest{}); _ = c.Snapshot() })
	}
	wg.Wait()
	if s = c.Snapshot(); s.ModelCalls != 19 || s.Usage.TotalTokens != 7 {
		t.Fatalf("%+v", s)
	}
}

func TestObservedPrivacyAndFirstText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	file, err := logging.Init(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	c := Observe(observationFake{chunks: []StreamChunk{{Choices: []Choice{{Delta: Delta{ReasoningContent: "SECRET_THOUGHT"}}}}}, err: errors.New("SECRET_KEY SECRET_BODY")})
	req := ChatRequest{Model: "model\nforged", Messages: []Message{{Content: "SECRET_PROMPT"}}}
	c.ChatStream(context.Background(), req, func(StreamChunk) error { return nil })
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "SECRET_") || strings.Contains(text, "first_text_ms") || strings.Contains(text, "model\nforged") {
		t.Fatal(text)
	}
	if !strings.Contains(text, `model="model\nforged"`) || !strings.Contains(text, `usage="unavailable"`) {
		t.Fatal(text)
	}
	c.upstream = observationFake{chunks: []StreamChunk{{Choices: []Choice{{Delta: Delta{Content: "SECRET_TEXT"}}}}}}
	c.ChatStream(context.Background(), req, func(StreamChunk) error { return nil })
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "first_text_ms=") || strings.Contains(string(b), "SECRET_TEXT") {
		t.Fatal(string(b))
	}
}

func TestProviderStreamUsage(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "responses"} {
		t.Run(provider, func(t *testing.T) {
			body := ""
			switch provider {
			case "openai":
				body = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"
			case "anthropic":
				body = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3},\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"
			case "responses":
				body = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				json.NewDecoder(r.Body).Decode(&req)
				if _, ok := req["stream_options"]; ok {
					t.Error("added incompatible stream option")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			var upstream AIClient
			switch provider {
			case "openai":
				upstream = NewClient(server.URL, "test")
			case "anthropic":
				upstream = NewAnthropicClient(server.URL, "test")
			case "responses":
				upstream = NewResponsesClient(server.URL, "test")
			}
			c := Observe(upstream)
			if err := c.ChatStream(context.Background(), ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hello"}}}, func(StreamChunk) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if s := c.Snapshot(); s.UsageCalls != 1 || s.Usage.TotalTokens != 5 || s.Usage.PromptTokens != 2 {
				t.Fatalf("%+v", s)
			}
		})
	}
}

func TestObservedCallbackErrorPassthrough(t *testing.T) {
	sentinel := errors.New("callback canceled")
	c := Observe(observationFake{chunks: []StreamChunk{{Usage: &Usage{Present: true, TotalTokens: 9}}}})
	if err := c.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) error { return sentinel }); err != sentinel {
		t.Fatal(err)
	}
	if s := c.Snapshot(); s.ModelCalls != 1 || s.UsageCalls != 1 || s.Usage.TotalTokens != 9 {
		t.Fatalf("%+v", s)
	}
}

func TestResponsesChatSSEUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n")
	}))
	defer server.Close()
	c := Observe(NewResponsesClient(server.URL, "test"))
	resp, err := c.Chat(context.Background(), ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Usage.Present || resp.Usage.TotalTokens != 5 {
		t.Fatalf("%+v", resp.Usage)
	}
}
