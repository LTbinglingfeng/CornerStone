package config

import "testing"

func TestNormalizeReplyWaitWindowMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "fixed", want: "fixed"},
		{input: " FIXED ", want: "fixed"},
		{input: "sliding", want: "sliding"},
		{input: "", want: "sliding"},
		{input: "other", want: "sliding"},
	}

	for _, tc := range tests {
		if got := normalizeReplyWaitWindowMode(tc.input); got != tc.want {
			t.Fatalf("normalizeReplyWaitWindowMode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeReplyWaitWindowSeconds(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{input: -1, want: 0},
		{input: 0, want: 0},
		{input: 2, want: 2},
		{input: MaxReplyWaitWindowSeconds + 1, want: MaxReplyWaitWindowSeconds},
	}

	for _, tc := range tests {
		if got := normalizeReplyWaitWindowSeconds(tc.input); got != tc.want {
			t.Fatalf("normalizeReplyWaitWindowSeconds(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeAssistantMessageSplitToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: DefaultAssistantMessageSplitToken, want: DefaultAssistantMessageSplitToken},
		{input: "  <sep>  ", want: "<sep>"},
		{input: "   ", want: ""},
		{input: "", want: ""},
	}

	for _, tc := range tests {
		if got := normalizeAssistantMessageSplitToken(tc.input); got != tc.want {
			t.Fatalf("normalizeAssistantMessageSplitToken(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
