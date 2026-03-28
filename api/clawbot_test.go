package api

import (
	"cornerstone/storage"
	"testing"
	"time"
)

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

func TestParseClawBotCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		ok      bool
		command clawBotCommand
	}{
		{
			name:    "list command",
			text:    "/ls",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandList},
		},
		{
			name:    "checkout command with index",
			text:    "／checkout  2",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandCheckout, Args: "2"},
		},
		{
			name:    "new command with punctuation",
			text:    "/new。",
			ok:      true,
			command: clawBotCommand{Name: clawBotCommandNew},
		},
		{
			name: "new command with content",
			text: "/new hello",
			ok:   true,
			command: clawBotCommand{
				Name: clawBotCommandNew,
				Args: "hello",
			},
		},
		{
			name: "not a command",
			text: "hello /ls",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			command, ok := parseClawBotCommand(tc.text)
			if ok != tc.ok {
				t.Fatalf("parseClawBotCommand(%q) ok = %v, want %v", tc.text, ok, tc.ok)
			}
			if !ok {
				return
			}
			if command != tc.command {
				t.Fatalf("parseClawBotCommand(%q) = %#v, want %#v", tc.text, command, tc.command)
			}
		})
	}
}

func TestResolveClawBotSessionSelector(t *testing.T) {
	sessions := []storage.ChatSession{
		{ID: "session-a"},
		{ID: "session-b"},
	}

	session, index, err := resolveClawBotSessionSelector(sessions, "2")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector index err = %v", err)
	}
	if index != 1 || session.ID != "session-b" {
		t.Fatalf("resolveClawBotSessionSelector index = (%d, %s), want (1, session-b)", index, session.ID)
	}

	session, index, err = resolveClawBotSessionSelector(sessions, "session-a")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector id err = %v", err)
	}
	if index != 0 || session.ID != "session-a" {
		t.Fatalf("resolveClawBotSessionSelector id = (%d, %s), want (0, session-a)", index, session.ID)
	}

	session, index, err = resolveClawBotSessionSelector(sessions, "b")
	if err != nil {
		t.Fatalf("resolveClawBotSessionSelector suffix err = %v", err)
	}
	if index != 1 || session.ID != "session-b" {
		t.Fatalf("resolveClawBotSessionSelector suffix = (%d, %s), want (1, session-b)", index, session.ID)
	}

	if _, _, err := resolveClawBotSessionSelector(sessions, "3"); err == nil {
		t.Fatal("resolveClawBotSessionSelector out of range error = nil, want error")
	}
}

func TestSplitClawBotReplyMessages(t *testing.T) {
	t.Run("split by assistant token", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("第一句→第二句→第三句", 2000)
		want := []string{"第一句", "第二句", "第三句"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("split token segments still obey max runes", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("12345→67890", 3)
		want := []string{"123", "45", "678", "90"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("fallback without split token", func(t *testing.T) {
		chunks := splitClawBotReplyMessages("abcdef", 4)
		want := []string{"abcd", "ef"}
		if len(chunks) != len(want) {
			t.Fatalf("splitClawBotReplyMessages len = %d, want %d", len(chunks), len(want))
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("splitClawBotReplyMessages[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})
}

func TestGetOrCreateActiveSessionRespectsPrompt(t *testing.T) {
	chatManager := storage.NewChatManager(t.TempDir())
	service := &ClawBotService{
		handler: &Handler{
			chatManager: chatManager,
		},
		activeSessions: make(map[string]*clawBotActiveSession),
	}

	userID := "wx-user"
	promptA := "prompt-a"
	promptB := "prompt-b"

	sessionA, err := chatManager.CreateSession(generateClawBotSessionID(userID), "Session A", promptA, "Prompt A")
	if err != nil {
		t.Fatalf("create session A err = %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	sessionB, err := chatManager.CreateSession(generateClawBotSessionID(userID), "Session B", promptB, "Prompt B")
	if err != nil {
		t.Fatalf("create session B err = %v", err)
	}

	service.touchActiveSession(userID, sessionB.SessionID)

	active, err := service.getOrCreateActiveSession(userID, promptA)
	if err != nil {
		t.Fatalf("getOrCreateActiveSession err = %v", err)
	}
	if active.SessionID != sessionA.SessionID {
		t.Fatalf("getOrCreateActiveSession session = %s, want %s", active.SessionID, sessionA.SessionID)
	}

	if currentID, ok := service.getActiveSessionID(userID); !ok || currentID != sessionA.SessionID {
		t.Fatalf("active session id = (%v, %s), want (true, %s)", ok, currentID, sessionA.SessionID)
	}
}
