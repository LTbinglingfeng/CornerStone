package api

import (
	"cornerstone/storage"
	"strings"
)

func assistantMessageSplitTokenPtr(splitToken string) *string {
	normalized := strings.TrimSpace(splitToken)
	return &normalized
}

func ensureAssistantMessageSplitToken(messages []storage.ChatMessage, splitToken string) {
	if len(messages) == 0 {
		return
	}

	tokenPtr := assistantMessageSplitTokenPtr(splitToken)
	for i := range messages {
		if strings.TrimSpace(messages[i].Role) != "assistant" {
			continue
		}
		if messages[i].AssistantMessageSplitToken != nil {
			continue
		}
		messages[i].AssistantMessageSplitToken = tokenPtr
	}
}
