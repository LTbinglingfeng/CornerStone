package storage

import (
	"cornerstone/client"
	"cornerstone/config"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixedTimeProvider struct {
	now time.Time
}

func (p fixedTimeProvider) Now() time.Time {
	return p.now
}

type memoryExtractorTestEnv struct {
	extractor  *MemoryExtractor
	mm         *MemoryManager
	promptID   string
	sessionID  string
	requestsCh chan string
}

func makeMemoryBatchUpsertToolCall(t *testing.T, id string, items []ExtractedMemory) client.ToolCall {
	t.Helper()

	args, err := json.Marshal(memoryBatchUpsertArgs{Items: items})
	if err != nil {
		t.Fatalf("marshal tool args: %v", err)
	}

	return client.ToolCall{
		ID:   id,
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      memoryBatchUpsertToolName,
			Arguments: string(args),
		},
	}
}

func makeOpenAIChatResponseJSON(t *testing.T, content string, toolCalls []client.ToolCall) string {
	t.Helper()

	body, err := json.Marshal(client.ChatResponse{
		ID:      "chatcmpl-memory-test",
		Object:  "chat.completion",
		Created: 123,
		Model:   "gpt-test",
		Choices: []client.Choice{
			{
				Index: 0,
				Message: client.Message{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: "tool_calls",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	return string(body)
}

func newMemoryExtractorTestEnv(t *testing.T, responseBody string, timeProvider TimeProvider) *memoryExtractorTestEnv {
	t.Helper()

	requestsCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		select {
		case requestsCh <- string(body):
		default:
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseBody)
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	cm := config.NewManager(filepath.Join(tempDir, "config.json"))
	cfg := config.DefaultConfig()
	cfg.MemoryEnabled = true
	cfg.MemoryProvider = &config.Provider{
		ID:      "memory",
		Name:    "Memory Provider",
		Type:    config.ProviderTypeOpenAI,
		BaseURL: server.URL,
		APIKey:  "secret-key",
		Model:   "gpt-test",
	}
	if err := cm.Update(cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}

	memoryDir := filepath.Join(tempDir, "prompts")
	mm := NewMemoryManager(memoryDir)

	promptID := "prompt_1"
	promptDir := filepath.Join(memoryDir, promptID)
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.json"), []byte(`{"id":"prompt_1","name":"小助手","content":"你是一个测试角色。"}`), 0644); err != nil {
		t.Fatalf("write prompt.json: %v", err)
	}

	chatMgr := NewChatManager(filepath.Join(tempDir, "chats"))
	sessionID := "session_1"
	if _, err := chatMgr.CreateSession(sessionID, "test", promptID, "小助手"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := chatMgr.AddMessage(sessionID, "user", "你好，我喜欢吃辣。"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if err := chatMgr.AddMessage(sessionID, "assistant", "记下了。"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	userMgr := NewUserManager(filepath.Join(tempDir, "user"))
	extractor := NewMemoryExtractor(mm, cm, chatMgr, userMgr, filepath.Join(tempDir, "memory_extraction_prompt.txt"), timeProvider)

	return &memoryExtractorTestEnv{
		extractor:  extractor,
		mm:         mm,
		promptID:   promptID,
		sessionID:  sessionID,
		requestsCh: requestsCh,
	}
}

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

func TestGetMemoryExtractionTools_ReturnsDeepCopy(t *testing.T) {
	if len(memoryExtractionTools) == 0 {
		t.Fatalf("expected memoryExtractionTools to be non-empty")
	}

	getItemsSchema := func(params map[string]interface{}) map[string]interface{} {
		t.Helper()

		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected properties map, got: %T", params["properties"])
		}
		items, ok := props["items"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected items schema map, got: %T", props["items"])
		}
		return items
	}

	getFirstCategoryEnum := func(params map[string]interface{}) interface{} {
		t.Helper()

		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected properties map, got: %T", params["properties"])
		}
		itemsSchema, ok := props["items"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected items schema map, got: %T", props["items"])
		}
		itemSchema, ok := itemsSchema["items"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected item schema map, got: %T", itemsSchema["items"])
		}
		oneOf, ok := itemSchema["oneOf"].([]interface{})
		if !ok || len(oneOf) == 0 {
			t.Fatalf("expected oneOf array, got: %T len=%d", itemSchema["oneOf"], len(oneOf))
		}
		alt0, ok := oneOf[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected oneOf[0] object, got: %T", oneOf[0])
		}
		altProps, ok := alt0["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected oneOf[0].properties map, got: %T", alt0["properties"])
		}
		category, ok := altProps["category"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected category schema map, got: %T", altProps["category"])
		}
		return category["enum"]
	}

	globalParams := memoryExtractionTools[0].Function.Parameters
	globalItems := getItemsSchema(globalParams)
	globalDesc, ok := globalItems["description"].(string)
	if !ok {
		t.Fatalf("expected global items.description string, got: %T", globalItems["description"])
	}

	tools1 := getMemoryExtractionTools()
	if len(tools1) != len(memoryExtractionTools) {
		t.Fatalf("unexpected tool count: got=%d want=%d", len(tools1), len(memoryExtractionTools))
	}

	// Mutate nested map in the returned copy.
	items1 := getItemsSchema(tools1[0].Function.Parameters)
	items1["description"] = "MUTATED"

	// Global schema must remain unchanged.
	globalItemsAfter := getItemsSchema(memoryExtractionTools[0].Function.Parameters)
	if got := globalItemsAfter["description"]; got != globalDesc {
		t.Fatalf("global schema mutated via returned tools: got=%v want=%v", got, globalDesc)
	}

	// Mutate a nested []string enum inside oneOf; it also must not leak back.
	enum1 := getFirstCategoryEnum(tools1[0].Function.Parameters)
	switch typed := enum1.(type) {
	case []string:
		if len(typed) == 0 {
			t.Fatalf("expected non-empty enum slice")
		}
		typed[0] = "MUTATED_ENUM"
	case []interface{}:
		if len(typed) == 0 {
			t.Fatalf("expected non-empty enum slice")
		}
		typed[0] = "MUTATED_ENUM"
	default:
		t.Fatalf("unexpected enum type: %T", enum1)
	}

	globalEnum := getFirstCategoryEnum(memoryExtractionTools[0].Function.Parameters)
	switch typed := globalEnum.(type) {
	case []string:
		if len(typed) == 0 {
			t.Fatalf("expected non-empty global enum slice")
		}
		if typed[0] == "MUTATED_ENUM" {
			t.Fatalf("global enum slice mutated via returned tools")
		}
	case []interface{}:
		if len(typed) == 0 {
			t.Fatalf("expected non-empty global enum slice")
		}
		if typed[0] == "MUTATED_ENUM" {
			t.Fatalf("global enum slice mutated via returned tools")
		}
	default:
		t.Fatalf("unexpected global enum type: %T", globalEnum)
	}

	// A second call must not observe previous mutations.
	tools2 := getMemoryExtractionTools()
	items2 := getItemsSchema(tools2[0].Function.Parameters)
	if got, ok := items2["description"].(string); !ok || got != globalDesc {
		t.Fatalf("second getMemoryExtractionTools call saw mutated description: got=%v want=%v", items2["description"], globalDesc)
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

func TestMemoryExtractor_ExtractAndSave_BatchToolCallAddsMultipleMemories(t *testing.T) {
	fixedNow := time.Date(2026, 4, 5, 13, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "call_1", []ExtractedMemory{
			{Subject: SubjectUser, Category: CategoryPreference, Content: "用户喜欢吃辣"},
			{Subject: SubjectSelf, Category: CategoryPromise, Content: "小助手答应下次提醒用户复习"},
		}),
	}), fixedTimeProvider{now: fixedNow})

	if err := env.extractor.ExtractAndSave(env.promptID, env.sessionID); err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	memories := env.mm.GetAll(env.promptID)
	if len(memories) != 2 {
		t.Fatalf("memories len = %d, want 2", len(memories))
	}

	if memories[0].CreatedAt != fixedNow || memories[0].LastSeen != fixedNow {
		t.Fatalf("first memory times = (%v, %v), want %v", memories[0].CreatedAt, memories[0].LastSeen, fixedNow)
	}
	if memories[1].CreatedAt != fixedNow || memories[1].LastSeen != fixedNow {
		t.Fatalf("second memory times = (%v, %v), want %v", memories[1].CreatedAt, memories[1].LastSeen, fixedNow)
	}

	var requestBody string
	select {
	case requestBody = <-env.requestsCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for request body")
	}
	if !strings.Contains(requestBody, `"name":"memory_batch_upsert"`) {
		t.Fatalf("request missing memory_batch_upsert tool: %s", requestBody)
	}
}

func TestMemoryExtractor_ExtractAndSave_BatchToolCallMixesUpdateAndAdd(t *testing.T) {
	oldCreatedAt := time.Date(2025, 12, 1, 9, 0, 0, 0, time.UTC)
	oldLastSeen := time.Date(2025, 12, 5, 9, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 4, 5, 14, 30, 0, 0, time.UTC)

	existing := Memory{
		ID:        "11111111-1111-1111-1111-111111111111",
		Subject:   SubjectUser,
		Category:  CategoryFact,
		Content:   "用户在上海工作",
		Strength:  0.40,
		Stability: 1.20,
		LastSeen:  oldLastSeen,
		SeenCount: 2,
		CreatedAt: oldCreatedAt,
	}
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "call_1", []ExtractedMemory{
			{
				MatchingID: func() *string {
					id := existing.ID
					return &id
				}(),
				Subject:  SubjectUser,
				Category: CategoryFact,
				Content:  "用户现在在杭州工作",
			},
			{Subject: SubjectUser, Category: CategoryPreference, Content: "用户周末喜欢徒步"},
		}),
	}), fixedTimeProvider{now: fixedNow})

	if err := env.mm.Add(env.promptID, existing); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	if err := env.extractor.ExtractAndSave(env.promptID, env.sessionID); err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	memories := env.mm.GetAll(env.promptID)
	if len(memories) != 2 {
		t.Fatalf("memories len = %d, want 2", len(memories))
	}

	updated := env.mm.FindByID(env.promptID, existing.ID)
	if updated == nil {
		t.Fatal("updated memory not found")
	}
	if updated.Content != "用户现在在杭州工作" {
		t.Fatalf("updated content = %q, want %q", updated.Content, "用户现在在杭州工作")
	}
	if updated.SeenCount != 3 {
		t.Fatalf("updated seen_count = %d, want 3", updated.SeenCount)
	}
	if math.Abs(updated.Strength-0.63) > 1e-9 {
		t.Fatalf("updated strength = %v, want 0.63", updated.Strength)
	}
	if math.Abs(updated.Stability-1.5) > 1e-9 {
		t.Fatalf("updated stability = %v, want 1.5", updated.Stability)
	}
	if updated.CreatedAt != oldCreatedAt {
		t.Fatalf("updated created_at = %v, want %v", updated.CreatedAt, oldCreatedAt)
	}
	if updated.LastSeen != fixedNow {
		t.Fatalf("updated last_seen = %v, want %v", updated.LastSeen, fixedNow)
	}

	var added *Memory
	for _, memory := range memories {
		if memory.ID != existing.ID {
			added = &memory
			break
		}
	}
	if added == nil {
		t.Fatal("added memory not found")
	}
	if added.CreatedAt != fixedNow || added.LastSeen != fixedNow {
		t.Fatalf("added memory times = (%v, %v), want %v", added.CreatedAt, added.LastSeen, fixedNow)
	}
}

func TestMemoryExtractor_ExtractAndSave_MergesMultipleToolCalls(t *testing.T) {
	fixedNow := time.Date(2026, 4, 5, 15, 0, 0, 0, time.UTC)
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, "这段文本应该被忽略", []client.ToolCall{
		makeMemoryBatchUpsertToolCall(t, "call_1", []ExtractedMemory{
			{Subject: SubjectUser, Category: CategoryPreference, Content: "用户喜欢手冲咖啡"},
		}),
		makeMemoryBatchUpsertToolCall(t, "call_2", []ExtractedMemory{
			{Subject: SubjectSelf, Category: CategoryPlan, Content: "小助手计划明天继续提醒用户运动"},
		}),
	}), fixedTimeProvider{now: fixedNow})

	if err := env.extractor.ExtractAndSave(env.promptID, env.sessionID); err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	memories := env.mm.GetAll(env.promptID)
	if len(memories) != 2 {
		t.Fatalf("memories len = %d, want 2", len(memories))
	}
}

func TestMemoryExtractor_ExtractAndSave_IgnoresTextJSONWithoutToolCalls(t *testing.T) {
	env := newMemoryExtractorTestEnv(t, makeOpenAIChatResponseJSON(t, `[{"subject":"user","category":"fact","content":"用户在广州工作"}]`, nil), fixedTimeProvider{
		now: time.Date(2026, 4, 5, 16, 0, 0, 0, time.UTC),
	})

	if err := env.extractor.ExtractAndSave(env.promptID, env.sessionID); err != nil {
		t.Fatalf("ExtractAndSave failed: %v", err)
	}

	if memories := env.mm.GetAll(env.promptID); len(memories) != 0 {
		t.Fatalf("memories len = %d, want 0", len(memories))
	}
}

func TestMemoryExtractor_DefaultTemplate_UsesToolOnlyBatchOnlySemantics(t *testing.T) {
	extractor := &MemoryExtractor{}
	template := extractor.GetDefaultTemplate()

	if !strings.Contains(template, memoryBatchUpsertToolName) {
		t.Fatalf("default template missing %q", memoryBatchUpsertToolName)
	}
	if !strings.Contains(template, "不允许输出普通文本") {
		t.Fatalf("default template missing text-only restriction: %q", template)
	}
	if !strings.Contains(template, "同一次 memory_batch_upsert 调用") {
		t.Fatalf("default template missing batch-only guidance: %q", template)
	}
	if strings.Contains(template, "返回 JSON 数组") {
		t.Fatalf("default template should not require JSON array output: %q", template)
	}
}

func TestMemoryExtractor_EnsureTemplateFile_MigratesLegacyDefaultTemplate(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "memory_extraction_prompt.txt")
	if err := os.WriteFile(templatePath, []byte(legacyDefaultMemoryExtractionPromptTemplate), 0644); err != nil {
		t.Fatalf("write legacy template: %v", err)
	}

	extractor := NewMemoryExtractor(NewMemoryManager(tempDir), nil, nil, nil, templatePath)
	got, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read migrated template: %v", err)
	}
	if string(got) != defaultMemoryExtractionPromptTemplate {
		t.Fatalf("migrated template mismatch")
	}
	if extractor.GetTemplate() != defaultMemoryExtractionPromptTemplate {
		t.Fatalf("GetTemplate() = %q, want default tool-only template", extractor.GetTemplate())
	}
}

func TestMemoryExtractor_EnsureTemplateFile_MigratesPreviousDefaultTemplate(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "memory_extraction_prompt.txt")
	if err := os.WriteFile(templatePath, []byte(previousDefaultMemoryExtractionPromptTemplate), 0644); err != nil {
		t.Fatalf("write previous default template: %v", err)
	}

	extractor := NewMemoryExtractor(NewMemoryManager(tempDir), nil, nil, nil, templatePath)
	got, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read migrated template: %v", err)
	}
	if string(got) != defaultMemoryExtractionPromptTemplate {
		t.Fatalf("migrated template mismatch")
	}
	if extractor.GetTemplate() != defaultMemoryExtractionPromptTemplate {
		t.Fatalf("GetTemplate() = %q, want latest default template", extractor.GetTemplate())
	}
}

func TestMemoryExtractor_EnsureTemplateFile_DoesNotOverrideCustomTemplate(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "memory_extraction_prompt.txt")
	customTemplate := `System:
自定义系统
{{EXISTING_MEMORIES}}
{{CHAT_CONTENT}}

User:
{{PERSONA}}`
	if err := os.WriteFile(templatePath, []byte(customTemplate), 0644); err != nil {
		t.Fatalf("write custom template: %v", err)
	}

	extractor := NewMemoryExtractor(NewMemoryManager(tempDir), nil, nil, nil, templatePath)
	got, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read custom template: %v", err)
	}
	if string(got) != customTemplate {
		t.Fatalf("custom template changed unexpectedly: %q", string(got))
	}
	if extractor.GetTemplate() != customTemplate {
		t.Fatalf("GetTemplate() = %q, want custom template", extractor.GetTemplate())
	}
}
