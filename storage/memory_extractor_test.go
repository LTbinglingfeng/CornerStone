package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitRoleTemplate(t *testing.T) {
	template := "System:\nS line\n\nUser:\nU line"
	systemPart, userPart, ok := splitRoleTemplate(template)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if systemPart != "S line" {
		t.Fatalf("unexpected system part: %q", systemPart)
	}
	if userPart != "U line" {
		t.Fatalf("unexpected user part: %q", userPart)
	}
}

func TestSplitRoleTemplate_FullWidthColonAndCaseInsensitive(t *testing.T) {
	template := "system：\nS\n\nUSER:\nU"
	systemPart, userPart, ok := splitRoleTemplate(template)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if systemPart != "S" || userPart != "U" {
		t.Fatalf("unexpected parts: system=%q user=%q", systemPart, userPart)
	}
}

func TestSplitRoleTemplate_RequiresBothNonEmpty(t *testing.T) {
	template := "System:\n\nUser:\nU"
	_, _, ok := splitRoleTemplate(template)
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestHasRoleTemplateHeader(t *testing.T) {
	if hasRoleTemplateHeader("hello") {
		t.Fatalf("expected false")
	}
	if !hasRoleTemplateHeader("System:\nhello") {
		t.Fatalf("expected true")
	}
	if !hasRoleTemplateHeader("User:\nhello") {
		t.Fatalf("expected true")
	}
}

func TestMemoryExtractor_BuildExtractionMessages_SplitTemplate(t *testing.T) {
	tempDir := t.TempDir()

	promptID := "test_prompt"
	promptDir := filepath.Join(tempDir, promptID)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	promptJSON := `{"id":"test_prompt","name":"小助手","content":"你是一个测试角色。"}`
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.json"), []byte(promptJSON), 0644); err != nil {
		t.Fatalf("write prompt.json: %v", err)
	}

	extractor := &MemoryExtractor{
		mm:           NewMemoryManager(tempDir),
		templatePath: "",
	}

	msgs := extractor.buildExtractionMessages(promptID, []ChatMessage{
		{Role: "user", Content: "你好", Timestamp: time.Now()},
		{Role: "assistant", Content: "你好呀", Timestamp: time.Now()},
	}, nil)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("unexpected roles: %+v", []string{msgs[0].Role, msgs[1].Role})
	}

	systemContent := msgs[0].Content
	userContent := msgs[1].Content

	if !strings.Contains(systemContent, "当前角色名字：小助手") {
		t.Fatalf("system missing avatar name: %q", systemContent)
	}
	if !strings.Contains(systemContent, "用户: 你好") || !strings.Contains(systemContent, "AI: 你好呀") {
		t.Fatalf("system missing chat content: %q", systemContent)
	}
	if strings.Contains(systemContent, MemoryExtractionPromptPlaceholderChatContent) {
		t.Fatalf("system still contains chat placeholder: %q", systemContent)
	}

	if !strings.Contains(userContent, "你是一个测试角色。") {
		t.Fatalf("user missing persona: %q", userContent)
	}
	if strings.Contains(userContent, MemoryExtractionPromptPlaceholderPersona) {
		t.Fatalf("user still contains persona placeholder: %q", userContent)
	}
}

func TestMemoryExtractor_UpdateTemplate_RequiresPersonaWhenRoleTemplate(t *testing.T) {
	tempDir := t.TempDir()
	extractor := &MemoryExtractor{
		templatePath: filepath.Join(tempDir, "memory_extraction_prompt.txt"),
	}

	template := `System:
{{EXISTING_MEMORIES}}
{{CHAT_CONTENT}}

User:
no persona here`

	if err := extractor.UpdateTemplate(template); err == nil {
		t.Fatalf("expected error")
	}
}
