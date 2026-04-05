package api

import (
	"context"
	"cornerstone/client"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeAIClient struct {
	t *testing.T

	requests  []client.ChatRequest
	responses []*client.ChatResponse
}

func (f *fakeAIClient) Chat(ctx context.Context, req client.ChatRequest) (*client.ChatResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return nil, errors.New("no more responses")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeAIClient) ChatStream(ctx context.Context, req client.ChatRequest, callback func(chunk client.StreamChunk) error) error {
	return errors.New("not implemented")
}

func toolCall(id, name, args string) client.ToolCall {
	return client.ToolCall{
		ID:   id,
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

func assistantMessage(content string, toolCalls ...client.ToolCall) client.Message {
	return client.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func chatResp(msg client.Message) *client.ChatResponse {
	return &client.ChatResponse{
		Choices: []client.Choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: "stop",
			},
		},
	}
}

func parseToolResult(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal tool result failed: %v raw=%s", err, raw)
	}
	return payload
}

func TestRunChatWithToolLoop_SuccessMultipleTools(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("tmp", []client.ToolCall{
				toolCall("call_pat", "send_pat", `{"name":"Alice","target":"Bob"}`),
				toolCall("call_rp", "send_red_packet", `{"amount":8.8,"message":"hi"}`),
			}...)),
			chatResp(assistantMessage("done")),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	got, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}}, executor, chatToolContext{}, nil)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got == nil || got.FinalResponse == nil {
		t.Fatalf("expected final response")
	}
	if got.ToolStepsUsed != 1 {
		t.Fatalf("ToolStepsUsed = %d, want 1", got.ToolStepsUsed)
	}
	if len(got.NewMessages) != 4 {
		t.Fatalf("NewMessages len=%d, want 4", len(got.NewMessages))
	}
	if got.NewMessages[0].Role != "assistant" || len(got.NewMessages[0].ToolCalls) != 2 {
		t.Fatalf("expected first new message to be assistant with 2 tool calls, got=%#v", got.NewMessages[0])
	}
	if got.NewMessages[1].Role != "tool" || got.NewMessages[1].ToolCallID != "call_pat" {
		t.Fatalf("expected tool message call_pat, got=%#v", got.NewMessages[1])
	}
	if got.NewMessages[2].Role != "tool" || got.NewMessages[2].ToolCallID != "call_rp" {
		t.Fatalf("expected tool message call_rp, got=%#v", got.NewMessages[2])
	}
	if got.NewMessages[3].Role != "assistant" || strings.TrimSpace(got.NewMessages[3].Content) != "done" {
		t.Fatalf("expected final assistant 'done', got=%#v", got.NewMessages[3])
	}

	tool1 := parseToolResult(t, got.NewMessages[1].Content)
	if ok, _ := tool1["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%v", tool1["ok"])
	}
	if tool, _ := tool1["tool"].(string); tool != "send_pat" {
		t.Fatalf("expected tool=send_pat, got=%v", tool1["tool"])
	}

	tool2 := parseToolResult(t, got.NewMessages[2].Content)
	if ok, _ := tool2["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%v", tool2["ok"])
	}
	if tool, _ := tool2["tool"].(string); tool != "send_red_packet" {
		t.Fatalf("expected tool=send_red_packet, got=%v", tool2["tool"])
	}

	if len(ai.requests) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(ai.requests))
	}
	secondReq := ai.requests[1]
	seenTool := false
	for _, msg := range secondReq.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "call_pat" {
			seenTool = true
		}
	}
	if !seenTool {
		t.Fatalf("expected second request to include tool message call_pat")
	}
}

func TestRunChatWithToolLoop_SyncNormalizedAssistantIntoFinalResponse(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(client.Message{Role: "", Content: "done"}),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	got, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}}, executor, chatToolContext{}, nil)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got == nil || got.FinalResponse == nil || len(got.FinalResponse.Choices) == 0 {
		t.Fatalf("expected final response")
	}
	if got.FinalResponse.Choices[0].Message.Role != "assistant" {
		t.Fatalf("FinalResponse role=%q, want assistant", got.FinalResponse.Choices[0].Message.Role)
	}
	if len(got.NewMessages) != 1 {
		t.Fatalf("NewMessages len=%d, want 1", len(got.NewMessages))
	}
	if got.NewMessages[0].Role != "assistant" {
		t.Fatalf("NewMessages[0] role=%q, want assistant", got.NewMessages[0].Role)
	}
}

func TestRunChatWithToolLoop_ToolFailureFedBack(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("", toolCall("call_bad", "send_red_packet", `not-json`))),
			chatResp(assistantMessage("ok")),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	got, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}}, executor, chatToolContext{}, nil)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got.ToolStepsUsed != 1 {
		t.Fatalf("ToolStepsUsed = %d, want 1", got.ToolStepsUsed)
	}

	if len(got.NewMessages) < 3 {
		t.Fatalf("expected at least 3 new messages, got %d", len(got.NewMessages))
	}
	toolMsg := got.NewMessages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_bad" {
		t.Fatalf("expected tool message call_bad, got=%#v", toolMsg)
	}
	payload := parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "send_red_packet" {
		t.Fatalf("expected tool=send_red_packet, got=%v", payload["tool"])
	}
}

func TestRunChatWithToolLoop_RejectsDisallowedToolCallUsingAllowedToolNames(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("", toolCall("call_disabled", "send_red_packet", `{"amount":8.8,"message":"hi"}`))),
			chatResp(assistantMessage("done")),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	handlerCalled := false
	executor.handlers["send_red_packet"] = func(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
		handlerCalled = true
		return chatToolResult{
			OK:   true,
			Data: map[string]interface{}{"unexpected": true},
		}
	}

	got, err := runChatWithToolLoop(
		context.Background(),
		ai,
		client.ChatRequest{
			Messages: []client.Message{{Role: "user", Content: "hi"}},
			Tools:    getChatTools(),
		},
		executor,
		chatToolContext{
			AllowedToolNames: map[string]bool{
				"send_pat": true,
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if handlerCalled {
		t.Fatalf("disabled tool handler should not be called")
	}
	if len(ai.requests) == 0 {
		t.Fatalf("expected at least one model request")
	}
	exposedToModel := false
	for _, tool := range ai.requests[0].Tools {
		if tool.Function.Name == "send_red_packet" {
			exposedToModel = true
			break
		}
	}
	if !exposedToModel {
		t.Fatalf("send_red_packet should still be exposed to the model")
	}
	if got == nil || len(got.NewMessages) < 3 {
		t.Fatalf("expected assistant, tool, assistant messages, got %#v", got)
	}

	toolMsg := got.NewMessages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_disabled" {
		t.Fatalf("expected tool message call_disabled, got=%#v", toolMsg)
	}
	payload := parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "send_red_packet" {
		t.Fatalf("expected tool=send_red_packet, got=%v", tool)
	}
	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "send_red_packet") {
		t.Fatalf("expected error to mention tool name, got=%q", errMsg)
	}
	if !strings.Contains(errMsg, "not allowed") && !strings.Contains(errMsg, "disabled") {
		t.Fatalf("expected error to mention disabled/not allowed, got=%q", errMsg)
	}
	if !strings.Contains(errMsg, "do not retry") {
		t.Fatalf("expected error to instruct model not to retry, got=%q", errMsg)
	}
	if !strings.Contains(errMsg, "enable") {
		t.Fatalf("expected error to suggest asking user to enable, got=%q", errMsg)
	}
}

func TestRunChatWithToolLoop_RejectsDisallowedWriteMemoryToolCallWhenToggleDisabled(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("", toolCall("call_memory", "write_memory", `{"items":[{"subject":"user","category":"preference","content":"喜欢黑咖啡"}]}`))),
			chatResp(assistantMessage("done")),
		},
	}

	availableTools := getChatTools(chatToolOptions{WriteMemoryEnabled: true})
	allowedToolNames := buildAllowedToolNameSet(availableTools, map[string]bool{
		"write_memory": false,
	})

	executor := newChatToolExecutor(nil, nil)
	handlerCalled := false
	executor.handlers["write_memory"] = func(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
		handlerCalled = true
		return chatToolResult{OK: true}
	}

	got, err := runChatWithToolLoop(
		context.Background(),
		ai,
		client.ChatRequest{
			Messages: []client.Message{{Role: "user", Content: "hi"}},
			Tools:    availableTools,
		},
		executor,
		chatToolContext{AllowedToolNames: allowedToolNames},
		nil,
	)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if handlerCalled {
		t.Fatal("write_memory handler should not be called when toggle is disabled")
	}
	if got == nil || len(got.NewMessages) < 2 {
		t.Fatalf("expected assistant and tool messages, got %#v", got)
	}

	toolMsg := got.NewMessages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_memory" {
		t.Fatalf("expected tool message call_memory, got=%#v", toolMsg)
	}
	payload := parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "write_memory" {
		t.Fatalf("expected tool=write_memory, got=%v", payload["tool"])
	}
	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "write_memory") {
		t.Fatalf("expected error to mention write_memory, got=%q", errMsg)
	}
	if !strings.Contains(errMsg, "do not retry") {
		t.Fatalf("expected error to instruct model not to retry, got=%q", errMsg)
	}
}

func TestRunChatWithToolLoop_TerminatesOnNoReply(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("", toolCall("call_no_reply", "no_reply", `{"reason":"生气了","cooldown_seconds":60}`))),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	finalAssistantCalls := 0
	var finalAssistant client.Message
	got, err := runChatWithToolLoop(
		context.Background(),
		ai,
		client.ChatRequest{
			Messages: []client.Message{{Role: "user", Content: "hi"}},
			Tools:    getChatTools(),
		},
		executor,
		chatToolContext{
			AllowedToolNames: map[string]bool{
				"no_reply": true,
			},
		},
		&toolLoopCallbacks{
			OnFinalAssistant: func(msg client.Message) {
				finalAssistantCalls++
				finalAssistant = msg
			},
		},
	)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got.FinalResponse != nil {
		t.Fatalf("FinalResponse = %#v, want nil", got.FinalResponse)
	}
	if got.ToolStepsUsed != 1 {
		t.Fatalf("ToolStepsUsed = %d, want 1", got.ToolStepsUsed)
	}
	if len(ai.requests) != 1 {
		t.Fatalf("model call count = %d, want 1", len(ai.requests))
	}
	if len(got.NewMessages) != 3 {
		t.Fatalf("NewMessages len=%d, want 3", len(got.NewMessages))
	}
	if got.NewMessages[0].Role != "assistant" || len(got.NewMessages[0].ToolCalls) != 1 || got.NewMessages[0].ToolCalls[0].Function.Name != "no_reply" {
		t.Fatalf("expected assistant no_reply tool call, got=%#v", got.NewMessages[0])
	}
	if got.NewMessages[1].Role != "tool" || got.NewMessages[1].ToolCallID != "call_no_reply" {
		t.Fatalf("expected tool message call_no_reply, got=%#v", got.NewMessages[1])
	}
	payload := parseToolResult(t, got.NewMessages[1].Content)
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "no_reply" {
		t.Fatalf("expected tool=no_reply, got=%v", payload["tool"])
	}
	data, _ := payload["data"].(map[string]interface{})
	if reason, _ := data["reason"].(string); reason != "生气了" {
		t.Fatalf("reason = %q, want %q", reason, "生气了")
	}
	if cooldown, _ := data["cooldown_seconds"].(float64); cooldown != 60 {
		t.Fatalf("cooldown_seconds = %v, want 60", cooldown)
	}
	if got.NewMessages[2].Role != "assistant" || got.NewMessages[2].Content != "" || len(got.NewMessages[2].ToolCalls) != 0 {
		t.Fatalf("expected silent final assistant, got=%#v", got.NewMessages[2])
	}
	if finalAssistantCalls != 1 {
		t.Fatalf("OnFinalAssistant call count = %d, want 1", finalAssistantCalls)
	}
	if finalAssistant.Role != "assistant" || finalAssistant.Content != "" || len(finalAssistant.ToolCalls) != 0 {
		t.Fatalf("OnFinalAssistant msg = %#v, want silent assistant", finalAssistant)
	}
}

func TestRunChatWithToolLoop_TerminatesOnNoReplyEvenWhenOtherToolsFail(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage(
				"",
				toolCall("call_no_reply", "no_reply", `{"reason":"生气了"}`),
				toolCall("call_weather", "get_weather", `{}`),
			)),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	got, err := runChatWithToolLoop(
		context.Background(),
		ai,
		client.ChatRequest{
			Messages: []client.Message{{Role: "user", Content: "hi"}},
			Tools:    getChatTools(),
		},
		executor,
		chatToolContext{
			AllowedToolNames: map[string]bool{
				"no_reply":    true,
				"get_weather": true,
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got.FinalResponse != nil {
		t.Fatalf("FinalResponse = %#v, want nil", got.FinalResponse)
	}
	if got.ToolStepsUsed != 1 {
		t.Fatalf("ToolStepsUsed = %d, want 1", got.ToolStepsUsed)
	}
	if len(ai.requests) != 1 {
		t.Fatalf("model call count = %d, want 1", len(ai.requests))
	}
	if len(got.NewMessages) != 4 {
		t.Fatalf("NewMessages len=%d, want 4", len(got.NewMessages))
	}

	toolMsg := got.NewMessages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_no_reply" {
		t.Fatalf("expected tool message call_no_reply, got=%#v", toolMsg)
	}
	payload := parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "no_reply" {
		t.Fatalf("expected tool=no_reply, got=%v", payload["tool"])
	}

	toolMsg = got.NewMessages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_weather" {
		t.Fatalf("expected tool message call_weather, got=%#v", toolMsg)
	}
	payload = parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "get_weather" {
		t.Fatalf("expected tool=get_weather, got=%v", payload["tool"])
	}

	if got.NewMessages[3].Role != "assistant" || got.NewMessages[3].Content != "" || len(got.NewMessages[3].ToolCalls) != 0 {
		t.Fatalf("expected silent final assistant, got=%#v", got.NewMessages[3])
	}
}

func TestRunChatWithToolLoop_DoesNotTerminateWhenNoReplyDisabled(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("", toolCall("call_no_reply", "no_reply", `{"reason":"生气了"}`))),
			chatResp(assistantMessage("我还是回你了。")),
		},
	}

	executor := newChatToolExecutor(nil, nil)
	got, err := runChatWithToolLoop(
		context.Background(),
		ai,
		client.ChatRequest{
			Messages: []client.Message{{Role: "user", Content: "hi"}},
			Tools:    getChatTools(),
		},
		executor,
		chatToolContext{
			AllowedToolNames: map[string]bool{
				"get_time": true,
			},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("runChatWithToolLoop err: %v", err)
	}
	if got.FinalResponse == nil || len(got.FinalResponse.Choices) == 0 {
		t.Fatalf("expected final response after disabled no_reply")
	}
	if got.ToolStepsUsed != 1 {
		t.Fatalf("ToolStepsUsed = %d, want 1", got.ToolStepsUsed)
	}
	if len(ai.requests) != 2 {
		t.Fatalf("model call count = %d, want 2", len(ai.requests))
	}
	if len(got.NewMessages) != 3 {
		t.Fatalf("NewMessages len=%d, want 3", len(got.NewMessages))
	}
	toolMsg := got.NewMessages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_no_reply" {
		t.Fatalf("expected tool message call_no_reply, got=%#v", toolMsg)
	}
	payload := parseToolResult(t, toolMsg.Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got=%v", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "no_reply" {
		t.Fatalf("expected tool=no_reply, got=%v", tool)
	}
	errMsg, _ := payload["error"].(string)
	if !strings.Contains(errMsg, "no_reply") || (!strings.Contains(errMsg, "disabled") && !strings.Contains(errMsg, "not allowed")) {
		t.Fatalf("expected disabled no_reply error, got=%q", errMsg)
	}
	if got.NewMessages[2].Role != "assistant" || got.NewMessages[2].Content != "我还是回你了。" {
		t.Fatalf("expected normal final assistant reply, got=%#v", got.NewMessages[2])
	}
}

func TestRunChatWithToolLoop_MaxToolSteps(t *testing.T) {
	steps := maxToolSteps + 1
	responses := make([]*client.ChatResponse, 0, steps)
	for i := 0; i < steps; i++ {
		responses = append(responses, chatResp(assistantMessage("", toolCall("call_loop", "send_pat", `{"name":"a","target":"b"}`))))
	}
	ai := &fakeAIClient{t: t, responses: responses}

	executor := newChatToolExecutor(nil, nil)
	_, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}}, executor, chatToolContext{}, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrToolLoopExceededMaxSteps) {
		t.Fatalf("expected ErrToolLoopExceededMaxSteps, got %v", err)
	}
	if len(ai.requests) != steps {
		t.Fatalf("expected %d model calls, got %d", steps, len(ai.requests))
	}
}
