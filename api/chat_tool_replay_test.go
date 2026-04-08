package api

import (
	"cornerstone/client"
	"testing"
)

func TestEnsureToolResultMessagesForReplay_MissingToolResultIsSyntheticFailure(t *testing.T) {
	in := []client.Message{
		assistantMessage(
			"tmp",
			toolCall("", "send_pat", `{"name":"Alice","target":"Bob"}`),
		),
		{Role: "assistant", Content: "next"},
	}

	out := ensureToolResultMessagesForReplay(in)
	if len(out) != 3 {
		t.Fatalf("out len=%d, want 3", len(out))
	}
	if out[0].Role != "assistant" || len(out[0].ToolCalls) != 1 {
		t.Fatalf("out[0] expected assistant with tool_calls, got=%#v", out[0])
	}
	if out[1].Role != "tool" {
		t.Fatalf("out[1].Role=%q, want tool", out[1].Role)
	}
	if out[1].ToolCallID == "" {
		t.Fatalf("out[1].ToolCallID empty")
	}

	payload := parseToolResult(t, out[1].Content)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("ok=%v, want false", payload["ok"])
	}
	if tool, _ := payload["tool"].(string); tool != "send_pat" {
		t.Fatalf("tool=%v, want send_pat", payload["tool"])
	}
	data, _ := payload["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("data is nil")
	}
	if replayed, _ := data["replayed"].(bool); !replayed {
		t.Fatalf("data.replayed=%v, want true", data["replayed"])
	}
	if synthetic, _ := data["synthetic"].(bool); !synthetic {
		t.Fatalf("data.synthetic=%v, want true", data["synthetic"])
	}
}

func TestNormalizeLegacyToolIdentifiersInMessages_RewritesWebSearchHistory(t *testing.T) {
	in := []client.Message{
		assistantMessage(
			"",
			toolCall("", legacyWebSearchToolName, `{"query":"hello"}`),
		),
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    `{"ok":true,"tool":"web_search","data":{"query":"hello"},"error":""}`,
		},
	}

	out := normalizeLegacyToolIdentifiersInMessages(in)
	if out[0].ToolCalls[0].Function.Name != cornerstoneWebSearchToolName {
		t.Fatalf("assistant tool name=%q, want %q", out[0].ToolCalls[0].Function.Name, cornerstoneWebSearchToolName)
	}

	payload := parseToolResult(t, out[1].Content)
	if tool, _ := payload["tool"].(string); tool != cornerstoneWebSearchToolName {
		t.Fatalf("tool=%v, want %s", payload["tool"], cornerstoneWebSearchToolName)
	}
}
