package api

import "testing"

func TestIsClawBotNewCommand(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		match bool
	}{
		{name: "plain", text: "/new", match: true},
		{name: "trim spaces", text: "  /new  ", match: true},
		{name: "full width slash", text: "／new", match: true},
		{name: "trailing chinese punctuation", text: "/new。", match: true},
		{name: "newline suffix", text: "/new\n", match: true},
		{name: "with content", text: "/new hello", match: false},
		{name: "different word", text: "/newchat", match: false},
		{name: "embedded", text: "hello /new", match: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClawBotNewCommand(tc.text); got != tc.match {
				t.Fatalf("isClawBotNewCommand(%q) = %v, want %v", tc.text, got, tc.match)
			}
		})
	}
}
