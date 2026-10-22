package client

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEmptyStreamUsageDoesNotOverwriteCounts(t *testing.T) {
	var empty Usage
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatal(err)
	}
	c := Observe(observationFake{chunks: []StreamChunk{
		{Usage: &Usage{Present: true, PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
		{Usage: &empty},
	}})
	if err := c.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot(); got.UsageCalls != 1 || got.Usage.TotalTokens != 5 {
		t.Fatalf("empty snapshot erased counts: %+v", got)
	}
}

func TestAnthropicAccountingIncludesCacheWithoutChangingAPI(t *testing.T) {
	var raw AnthropicUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":200,"cache_read_input_tokens":1000}`), &raw); err != nil {
		t.Fatal(err)
	}
	response := &ChatResponse{Usage: anthropicUsage(&raw)}
	c := Observe(observationFake{response: response})
	got, err := c.Chat(context.Background(), ChatRequest{})
	if err != nil || got != response || got.Usage.PromptTokens != 10 || got.Usage.TotalTokens != 30 {
		t.Fatalf("changed API usage: %+v %v", got, err)
	}
	stats := c.Snapshot()
	if stats.Usage.PromptTokens != 1210 || stats.Usage.TotalTokens != 1230 {
		t.Fatalf("cache inputs omitted: %+v", stats)
	}
}

func TestAnthropicUsagePresenceAndCumulativeDelta(t *testing.T) {
	for _, body := range []string{`{}`, `{"input_tokens":null,"output_tokens":null}`} {
		var u AnthropicUsage
		if err := json.Unmarshal([]byte(body), &u); err != nil {
			t.Fatal(err)
		}
		if anthropicUsage(&u).Present {
			t.Fatalf("empty usage counted: %s", body)
		}
	}
	var u AnthropicUsage
	json.Unmarshal([]byte(`{"input_tokens":2,"output_tokens":0}`), &u)
	json.Unmarshal([]byte(`{"output_tokens":3}`), &u)
	if got := anthropicUsage(&u); !got.Present || got.PromptTokens != 2 || got.CompletionTokens != 3 {
		t.Fatalf("lost initial input: %+v", got)
	}
}
