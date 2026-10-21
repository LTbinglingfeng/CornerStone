package api

import (
	"context"
	"cornerstone/client"
	"encoding/json"
	"testing"
	"time"
)

func TestProviderStateToolContinuationAndHistory(t *testing.T) {
	state := &client.ProviderState{Provider: "gemini", Model: "model", Payload: json.RawMessage(`[{"functionCall":{"id":"call","name":"send_pat","args":{}},"thoughtSignature":"c2ln"}]`)}
	assistant := assistantMessage("", toolCall("call", "send_pat", `{"name":"Alice","target":"Bob"}`))
	assistant.ProviderState = state
	ai := &fakeAIClient{t: t, responses: []*client.ChatResponse{chatResp(assistant), chatResp(assistantMessage("done"))}}
	got, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Model: "model", Messages: []client.Message{{Role: "user", Content: "hello"}}}, newChatToolExecutor(), chatToolContext{}, &toolLoopCallbacks{OnToolStep: func(_ int, m client.Message) { m.ProviderState.Payload[0] = 'x' }})
	if err != nil {
		t.Fatal(err)
	}
	if ai.requests[1].Messages[1].ProviderState.Payload[0] != '[' {
		t.Fatal("callback mutated continuation")
	}
	stored := buildGeneratedReplyStorageMessages(got, "done", sessionReplyPersistAll, time.Now())
	restored := ensureToolResultMessagesForReplay(convertChatMessages(stored))
	if restored[0].ProviderState == nil || string(restored[0].ProviderState.Payload) != string(state.Payload) {
		t.Fatal("history lost continuation data")
	}
	restored[0].ProviderState.Payload[0] = 'x'
	if stored[0].ProviderState.Payload[0] != '[' || got.NewMessages[0].ProviderState.Payload[0] != '[' {
		t.Fatal("conversion aliases state")
	}
}
