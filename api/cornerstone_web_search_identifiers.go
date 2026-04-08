package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"encoding/json"
	"strings"
)

const (
	cornerstoneWebSearchToolName           = config.CornerstoneWebSearchKey
	legacyWebSearchToolName                = config.LegacyWebSearchKey
	cornerstoneWebSearchSettingsPath       = "/api/settings/cornerstone-web-search"
	legacyCornerstoneWebSearchSettingsPath = "/api/settings/web-search"
	cornerstoneWebSearchStartedEventType   = "cornerstone_web_search_started"
	cornerstoneWebSearchCompletedEventType = "cornerstone_web_search_completed"
)

func canonicalToolName(name string) string {
	switch strings.TrimSpace(name) {
	case legacyWebSearchToolName:
		return cornerstoneWebSearchToolName
	default:
		return strings.TrimSpace(name)
	}
}

func normalizeLegacyToolIdentifiersInTools(tools []client.Tool) []client.Tool {
	if len(tools) == 0 {
		return tools
	}

	normalized := make([]client.Tool, len(tools))
	copy(normalized, tools)

	changed := false
	for i := range normalized {
		nextName := canonicalToolName(normalized[i].Function.Name)
		if nextName == normalized[i].Function.Name {
			continue
		}
		normalized[i].Function.Name = nextName
		changed = true
	}
	if !changed {
		return tools
	}
	return normalized
}

func normalizeToolResultPayloadToolName(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, false
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return raw, false
	}

	currentTool, _ := payload["tool"].(string)
	nextTool := canonicalToolName(currentTool)
	if nextTool == currentTool {
		return raw, false
	}
	payload["tool"] = nextTool

	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func normalizeLegacyToolIdentifiersInMessages(messages []client.Message) []client.Message {
	if len(messages) == 0 {
		return messages
	}

	normalized := make([]client.Message, len(messages))
	copy(normalized, messages)

	changed := false
	for i := range normalized {
		if len(normalized[i].ToolCalls) > 0 {
			calls := append([]client.ToolCall(nil), normalized[i].ToolCalls...)
			localChanged := false
			for j := range calls {
				nextName := canonicalToolName(calls[j].Function.Name)
				if nextName == calls[j].Function.Name {
					continue
				}
				calls[j].Function.Name = nextName
				localChanged = true
			}
			if localChanged {
				normalized[i].ToolCalls = calls
				changed = true
			}
		}

		if normalized[i].Role == "tool" {
			nextContent, contentChanged := normalizeToolResultPayloadToolName(normalized[i].Content)
			if contentChanged {
				normalized[i].Content = nextContent
				changed = true
			}
		}
	}

	if !changed {
		return messages
	}
	return normalized
}
