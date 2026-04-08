package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"errors"
	"fmt"
	"strings"
)

const maxToolSteps = 10

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
	allowedToolNames := toolCtx.AllowedToolNames
	enforceToolAllowlist := allowedToolNames != nil

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
				assistant.ToolCalls[i].Function.Name = canonicalToolName(assistant.ToolCalls[i].Function.Name)
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
		shouldTerminateWithNoReply := false
		for index, tc := range assistant.ToolCalls {
			callID := strings.TrimSpace(tc.ID)
			if callID == "" {
				callID = fmt.Sprintf("call_%d_%d", toolStepsUsed, index)
			}
			toolName := canonicalToolName(tc.Function.Name)

			result := chatToolResult{}
			if _, dup := executedToolCallIDs[callID]; dup {
				// no_reply has no side effects; treat duplicate ids as a no-op success so it can
				// still end the round silently.
				if toolName == "no_reply" {
					result = chatToolResult{
						OK:    true,
						Tool:  toolName,
						Data:  nil,
						Error: "",
					}
				} else {
					result = chatToolResult{
						OK:    false,
						Tool:  toolName,
						Data:  nil,
						Error: "duplicate tool_call_id",
					}
				}
			} else if enforceToolAllowlist && !isToolAvailable(allowedToolNames, toolName) {
				executedToolCallIDs[callID] = struct{}{}
				result = chatToolResult{
					OK:    false,
					Tool:  toolName,
					Data:  nil,
					Error: fmt.Sprintf("tool %q is disabled or not allowed by current user settings; do not retry it. Ask the user to enable it or continue without this tool.", toolName),
				}
			} else {
				executedToolCallIDs[callID] = struct{}{}
				result = toolExecutor.ExecuteResult(ctx, tc, toolCtx)
			}
			if toolName == "no_reply" && result.OK {
				shouldTerminateWithNoReply = true
			}
			resultJSON := marshalChatToolResult(result)

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
		if shouldTerminateWithNoReply {
			silentAssistant := client.Message{
				Role:    "assistant",
				Content: "",
			}
			conversation = append(conversation, silentAssistant)
			newMessages = append(newMessages, silentAssistant)
			if callbacks != nil && callbacks.OnFinalAssistant != nil {
				callbacks.OnFinalAssistant(silentAssistant)
			}
			return &toolLoopResult{
				FinalResponse: nil,
				NewMessages:   newMessages,
				ToolStepsUsed: toolStepsUsed,
			}, nil
		}
	}
}
