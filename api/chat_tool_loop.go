package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"errors"
	"fmt"
	"strings"
)

const maxToolSteps = 5

var ErrToolLoopExceededMaxSteps = errors.New("tool loop exceeded max steps")

type toolLoopCallbacks struct {
	// Called when an assistant turn requests tools (step starts at 1).
	OnToolStep func(step int, assistant client.Message)
	// Called for each tool message produced by executing tools.
	OnToolMessage func(step int, msg client.Message)
	// Called when the final assistant message is produced.
	OnFinalAssistant func(msg client.Message)
}

type toolLoopResult struct {
	FinalResponse *client.ChatResponse
	NewMessages   []client.Message // assistant(tool_calls) -> tool -> assistant(final), in order
	ToolStepsUsed int
}

func runChatWithToolLoop(
	ctx context.Context,
	aiClient client.AIClient,
	baseReq client.ChatRequest,
	toolExecutor *chatToolExecutor,
	toolCtx chatToolContext,
	callbacks *toolLoopCallbacks,
) (*toolLoopResult, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("missing ai client")
	}
	if toolExecutor == nil {
		return nil, fmt.Errorf("missing tool executor")
	}

	conversation := append([]client.Message(nil), baseReq.Messages...)
	newMessages := make([]client.Message, 0, 4)
	allowedToolNames := buildToolNameSet(baseReq.Tools)
	enforceToolAllowlist := baseReq.Tools != nil

	toolStepsUsed := 0
	executedToolCallIDs := make(map[string]struct{})

	for {
		req := baseReq
		req.Stream = false
		req.Messages = conversation

		resp, errChat := aiClient.Chat(ctx, req)
		if errChat != nil {
			return &toolLoopResult{
				FinalResponse: nil,
				NewMessages:   newMessages,
				ToolStepsUsed: toolStepsUsed,
			}, errChat
		}
		if resp == nil || len(resp.Choices) == 0 {
			return &toolLoopResult{
				FinalResponse: nil,
				NewMessages:   newMessages,
				ToolStepsUsed: toolStepsUsed,
			}, fmt.Errorf("empty response")
		}

		assistant := resp.Choices[0].Message
		if strings.TrimSpace(assistant.Role) == "" {
			assistant.Role = "assistant"
		}

		// Ensure every tool call has a stable ID so tool results can be matched.
		if len(assistant.ToolCalls) > 0 {
			for i := range assistant.ToolCalls {
				id := strings.TrimSpace(assistant.ToolCalls[i].ID)
				if id != "" {
					continue
				}
				assistant.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", toolStepsUsed+1, i)
			}
		}

		resp.Choices[0].Message = assistant

		conversation = append(conversation, assistant)
		newMessages = append(newMessages, assistant)

		if len(assistant.ToolCalls) == 0 {
			if callbacks != nil && callbacks.OnFinalAssistant != nil {
				callbacks.OnFinalAssistant(assistant)
			}
			return &toolLoopResult{
				FinalResponse: resp,
				NewMessages:   newMessages,
				ToolStepsUsed: toolStepsUsed,
			}, nil
		}

		toolStepsUsed++
		if toolStepsUsed > maxToolSteps {
			logging.Errorf("tool loop exceeded max steps: max=%d used=%d", maxToolSteps, toolStepsUsed)
			return &toolLoopResult{
				FinalResponse: nil,
				NewMessages:   newMessages,
				ToolStepsUsed: toolStepsUsed,
			}, fmt.Errorf("%w: max=%d", ErrToolLoopExceededMaxSteps, maxToolSteps)
		}

		if callbacks != nil && callbacks.OnToolStep != nil {
			callbacks.OnToolStep(toolStepsUsed, assistant)
		}

		toolMessages := make([]client.Message, 0, len(assistant.ToolCalls))
		for index, tc := range assistant.ToolCalls {
			callID := strings.TrimSpace(tc.ID)
			if callID == "" {
				callID = fmt.Sprintf("call_%d_%d", toolStepsUsed, index)
			}

			resultJSON := ""
			if _, dup := executedToolCallIDs[callID]; dup {
				resultJSON = marshalChatToolResult(chatToolResult{
					OK:    false,
					Tool:  strings.TrimSpace(tc.Function.Name),
					Data:  nil,
					Error: "duplicate tool_call_id",
				})
			} else if enforceToolAllowlist && !isToolAvailable(allowedToolNames, tc.Function.Name) {
				executedToolCallIDs[callID] = struct{}{}
				resultJSON = marshalChatToolResult(chatToolResult{
					OK:    false,
					Tool:  strings.TrimSpace(tc.Function.Name),
					Data:  nil,
					Error: "tool disabled",
				})
			} else {
				executedToolCallIDs[callID] = struct{}{}
				resultJSON = toolExecutor.Execute(ctx, tc, toolCtx)
			}

			toolMsg := client.Message{
				Role:       "tool",
				Content:    resultJSON,
				ToolCallID: callID,
			}
			toolMessages = append(toolMessages, toolMsg)
			newMessages = append(newMessages, toolMsg)
			if callbacks != nil && callbacks.OnToolMessage != nil {
				callbacks.OnToolMessage(toolStepsUsed, toolMsg)
			}
		}

		conversation = append(conversation, toolMessages...)
	}
}
