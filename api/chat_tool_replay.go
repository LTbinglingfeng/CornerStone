package api

import (
	"cornerstone/client"
	"fmt"
	"strings"
)

// ensureToolResultMessagesForReplay patches legacy histories that contain assistant tool_calls
// but do not include the corresponding role=tool messages.
//
// It does NOT execute tools (no side effects). It only inserts synthetic tool result messages
// so providers that validate tool call traces can continue the conversation.
func ensureToolResultMessagesForReplay(messages []client.Message) []client.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]client.Message, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistant := msg
			expected := make([]struct {
				id   string
				tool string
			}, 0, len(msg.ToolCalls))

			for j := range assistant.ToolCalls {
				callID := strings.TrimSpace(assistant.ToolCalls[j].ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d_%d", i, j)
					assistant.ToolCalls[j].ID = callID
				}
				expected = append(expected, struct {
					id   string
					tool string
				}{
					id:   callID,
					tool: strings.TrimSpace(assistant.ToolCalls[j].Function.Name),
				})
			}

			out = append(out, assistant)

			seen := make(map[string]struct{})
			for k := i + 1; k < len(messages); k++ {
				if messages[k].Role != "tool" {
					break
				}
				id := strings.TrimSpace(messages[k].ToolCallID)
				if id != "" {
					seen[id] = struct{}{}
				}
			}

			for _, exp := range expected {
				if exp.id == "" {
					continue
				}
				if _, ok := seen[exp.id]; ok {
					continue
				}
				out = append(out, client.Message{
					Role:       "tool",
					ToolCallID: exp.id,
					Content: marshalChatToolResult(chatToolResult{
						OK:    true,
						Tool:  exp.tool,
						Data:  map[string]interface{}{"replayed": true},
						Error: "",
					}),
				})
			}

			continue
		}

		out = append(out, msg)
	}

	return out
}
