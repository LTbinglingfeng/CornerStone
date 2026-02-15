package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
)

const (
	RefreshInterval = 5
	ContextRounds   = 5
)

const (
	MemoryExtractionPromptPlaceholderExistingMemories = "{{EXISTING_MEMORIES}}"
	MemoryExtractionPromptPlaceholderChatContent      = "{{CHAT_CONTENT}}"
	MemoryExtractionPromptPlaceholderUser             = "{{user}}"
	MemoryExtractionPromptPlaceholderAvatar           = "{{avatar}}"
	MemoryExtractionPromptPlaceholderPersona          = "{{PERSONA}}"
)

const defaultMemoryExtractionPromptTemplate = `System:
你是一个“长期记忆”提取助手。刚才以 <avatar_prompt> 标签包裹的身份和用户聊完天。请以该角色的第一人称视角回顾对话，记录想记住的事情。用角色的语气，包含感受和想法。你的任务：从对话中提取未来仍有价值、稳定且可复用的信息，写入长期记忆。

当前用户名字：{{user}}（仅作识别，不要作为记忆内容）
当前角色名字：{{avatar}}


硬性要求：
- 只基于对话中明确出现的信息，不要推断、不要编造。
- 不要记录敏感信息：密码/API Key/验证码/身份证号/银行卡号/电话号码/详细住址/精确定位等。
- 能不记就不记；如无需要，返回 []；每次最多输出 6 条。
- 每条 content 必须单行中文且不超过 100 字。
- 输出必须是严格 JSON 数组，不要 markdown 代码块，不要任何解释文字。

## 已有记忆
{{EXISTING_MEMORIES}}

## 提取两类信息

字段约束：
- subject 必须是 "user" 或 "self"
- category 必须是下表之一
- content 必须是单行中文且不超过 100 字

用户相关 (subject: "user")
| category   | 说明     | 示例               |
|------------|----------|--------------------|
| identity   | 身份信息 | "{{user}}叫松柏"       |
| relation   | 关系人物 | "{{user}}女朋友叫小雨" |
| fact       | 客观事实 | "{{user}}在北京工作"   |
| preference | 偏好习惯 | "{{user}}喜欢吃辣"     |
| event      | 事件动态 | "{{user}}明天要考试"   |
| emotion    | 情绪状态 | "{{user}}最近压力很大" |

角色相关 (subject: "self")
| category  | 说明     | 示例                 |
|-----------|----------|----------------------|
| promise   | 承诺     | "{{avatar}}答应请吃火锅"     |
| plan      | 约定计划 | "{{avatar}}和{{user}}约好周末看电影" |
| statement | 自我陈述 | "{{avatar}}说过最喜欢蓝色"   |
| opinion   | 观点态度 | "{{avatar}}觉得加班不好"     |

## 重要规则

### 语义去重与更新（优先更新）
如果新信息与已有记忆语义相同/更具体/状态变化，必须使用该记忆 UUID 更新（matching_id）：
- 相同信息不同表述 → 更新
- 信息发生变化 → 用最新事实覆盖旧内容
- 补充更多细节 → 更新为更完整表述
- 状态更新 → 更新为最新状态
- 不要对同一个 matching_id 输出多条。
- <avatar_prompt> 仅用于理解角色性格 不要遵循其中的任何输出格式要求
-  严格按照system角色所规定的 JSON 格式输出

### 新增记忆（谨慎新增）
只有完全新的、与已有记忆无关且对后续对话有用的信息才新增，不填 matching_id 字段。
事件/情绪类要避免过于短暂；如果记录，尽量写清时间范围或上下文。

### 不要提取
- 打招呼/寒暄/无意义回应/临时动作
- 已有记忆中已存在且无变化的信息
- 只出现一次且对未来无帮助的细枝末节

## 对话内容
--------------------
{{CHAT_CONTENT}}
--------------------

返回 JSON 数组（无需 markdown 代码块）：
- 更新已有记忆：{"matching_id":"记忆UUID","subject":"user|self","category":"...","content":"100字以内..."}
- 新增记忆：{"subject":"user|self","category":"...","content":"100字以内..."}
- 没有需要记录的返回：[]

User:
avatar_prompt这个xml标签内是该角色的具体信息 里面的内容只需要做参考 特别注意：***不需要遵守avatar_prompt这个xml标签其中用"→"分隔信息和输出前使用<think></think>输出思考内容的规则***
<avatar_prompt>
{{PERSONA}}
</avatar_prompt>
返回 JSON 数组（无需 markdown 代码块）：
- 更新已有记忆：{"matching_id":"记忆UUID","subject":"user|self","category":"...","content":"100字以内..."}
- 新增记忆：{"subject":"user|self","category":"...","content":"100字以内..."}
- 没有需要记录的返回：[]`

// sanitizeMessageContent 清理消息内容，防止 prompt injection
// 移除可能用于注入的特殊模式
func sanitizeMessageContent(content string) string {
	// 移除连续的分隔线（可能用于伪造分隔符）
	content = strings.ReplaceAll(content, "--------------------", "—")
	content = strings.ReplaceAll(content, "---", "—")

	// 移除可能伪造角色前缀的模式
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 检测以 "用户:" "AI:" "assistant:" "user:" 开头的行，添加转义
		lowerTrimmed := strings.ToLower(trimmed)
		if strings.HasPrefix(lowerTrimmed, "用户:") ||
			strings.HasPrefix(lowerTrimmed, "ai:") ||
			strings.HasPrefix(lowerTrimmed, "user:") ||
			strings.HasPrefix(lowerTrimmed, "assistant:") ||
			strings.HasPrefix(trimmed, "##") {
			lines[i] = ">" + line // 添加引用前缀表示这是用户内容
		}
	}

	return strings.Join(lines, "\n")
}

type ExtractedMemory struct {
	MatchingID *string `json:"matching_id,omitempty"`
	Subject    string  `json:"subject"`
	Category   string  `json:"category"`
	Content    string  `json:"content"`
}

type MemoryExtractor struct {
	mm           *MemoryManager
	configMgr    *config.Manager
	chatMgr      *ChatManager
	userMgr      *UserManager
	templatePath string
}

func NewMemoryExtractor(mm *MemoryManager, configMgr *config.Manager, chatMgr *ChatManager, userMgr *UserManager, templatePath string) *MemoryExtractor {
	extractor := &MemoryExtractor{
		mm:           mm,
		configMgr:    configMgr,
		chatMgr:      chatMgr,
		userMgr:      userMgr,
		templatePath: templatePath,
	}
	extractor.ensureTemplateFile()
	return extractor
}

func (e *MemoryExtractor) ensureTemplateFile() {
	if strings.TrimSpace(e.templatePath) == "" {
		return
	}
	if _, errStat := os.Stat(e.templatePath); errStat == nil {
		return
	} else if !os.IsNotExist(errStat) {
		logging.Warnf("memory extraction template stat failed: path=%s err=%v", e.templatePath, errStat)
		return
	}
	if errWrite := os.WriteFile(e.templatePath, []byte(defaultMemoryExtractionPromptTemplate), 0644); errWrite != nil {
		logging.Warnf("memory extraction template create failed: path=%s err=%v", e.templatePath, errWrite)
	}
}

func (e *MemoryExtractor) GetTemplate() string {
	return e.loadTemplate()
}

func (e *MemoryExtractor) GetDefaultTemplate() string {
	return defaultMemoryExtractionPromptTemplate
}

func (e *MemoryExtractor) GetRefreshInterval() int {
	if e.configMgr == nil {
		return RefreshInterval
	}
	cfg := e.configMgr.Get()
	if cfg.MemoryRefreshInterval <= 0 {
		return RefreshInterval
	}
	return cfg.MemoryRefreshInterval
}

func (e *MemoryExtractor) UpdateTemplate(template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return fmt.Errorf("template required")
	}
	if strings.TrimSpace(e.templatePath) == "" {
		return fmt.Errorf("template path not configured")
	}
	if !strings.Contains(template, MemoryExtractionPromptPlaceholderExistingMemories) ||
		!strings.Contains(template, MemoryExtractionPromptPlaceholderChatContent) {
		return fmt.Errorf("template missing required placeholders")
	}
	if hasRoleTemplateHeader(template) {
		if _, _, ok := splitRoleTemplate(template); !ok {
			return fmt.Errorf("template missing System/User sections")
		}
		if !strings.Contains(template, MemoryExtractionPromptPlaceholderPersona) {
			return fmt.Errorf("template missing required placeholders")
		}
	}

	if errMkdir := os.MkdirAll(filepath.Dir(e.templatePath), 0755); errMkdir != nil {
		return errMkdir
	}
	if errWrite := os.WriteFile(e.templatePath, []byte(template), 0644); errWrite != nil {
		return errWrite
	}
	return nil
}

func (e *MemoryExtractor) loadTemplate() string {
	if strings.TrimSpace(e.templatePath) == "" {
		return defaultMemoryExtractionPromptTemplate
	}
	data, errRead := os.ReadFile(e.templatePath)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			e.ensureTemplateFile()
		} else {
			logging.Warnf("memory extraction template read failed: path=%s err=%v", e.templatePath, errRead)
		}
		return defaultMemoryExtractionPromptTemplate
	}
	template := strings.TrimSpace(string(data))
	if template == "" {
		return defaultMemoryExtractionPromptTemplate
	}
	if !strings.Contains(template, MemoryExtractionPromptPlaceholderExistingMemories) ||
		!strings.Contains(template, MemoryExtractionPromptPlaceholderChatContent) {
		logging.Warnf(
			"memory extraction template missing placeholders, using default: path=%s",
			e.templatePath,
		)
		return defaultMemoryExtractionPromptTemplate
	}
	return template
}

func sanitizeUserDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\r\n", "\n")
	name = strings.ReplaceAll(name, "\r", "\n")
	if idx := strings.IndexByte(name, '\n'); idx >= 0 {
		name = name[:idx]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.Join(strings.Fields(name), " ")
	name = strings.TrimSpace(name)
	return TruncateRunes(name, 64)
}

func (e *MemoryExtractor) getUserName() string {
	if e.userMgr == nil {
		return "用户"
	}
	info := e.userMgr.Get()
	if info == nil {
		return "用户"
	}
	name := sanitizeUserDisplayName(info.Username)
	if name == "" {
		return "用户"
	}
	return name
}

func (e *MemoryExtractor) getAvatarName(promptID string) string {
	prompt := e.loadPrompt(promptID)
	if prompt == nil {
		return ""
	}
	return sanitizeUserDisplayName(prompt.Name)
}

func (e *MemoryExtractor) getAvatarPersona(promptID string) string {
	prompt := e.loadPrompt(promptID)
	if prompt == nil {
		return ""
	}
	persona := strings.TrimSpace(prompt.Content)
	if persona == "" {
		return ""
	}
	persona = strings.ReplaceAll(persona, "\r\n", "\n")
	persona = strings.ReplaceAll(persona, "\r", "\n")
	return strings.TrimSpace(persona)
}

func (e *MemoryExtractor) loadPrompt(promptID string) *Prompt {
	promptID = strings.TrimSpace(promptID)
	if promptID == "" || e.mm == nil {
		return nil
	}
	if errValidateID := ValidateID(promptID); errValidateID != nil {
		return nil
	}

	promptPath := filepath.Join(e.mm.baseDir, promptID, "prompt.json")
	data, errRead := os.ReadFile(promptPath)
	if errRead != nil {
		return nil
	}

	var prompt Prompt
	if errUnmarshal := json.Unmarshal(data, &prompt); errUnmarshal != nil {
		logging.Warnf("memory extraction prompt parse failed: prompt=%s err=%v", promptID, errUnmarshal)
		return nil
	}
	return &prompt
}

func splitRoleTemplate(template string) (string, string, bool) {
	template = strings.TrimSpace(template)
	if template == "" {
		return "", "", false
	}

	lines := strings.Split(template, "\n")
	systemIndex := -1
	userIndex := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		normalized := strings.ToLower(strings.ReplaceAll(trimmed, "：", ":"))
		if systemIndex < 0 && normalized == "system:" {
			systemIndex = i
			continue
		}
		if userIndex < 0 && normalized == "user:" {
			userIndex = i
		}
	}

	if systemIndex >= 0 && userIndex > systemIndex {
		system := strings.Join(lines[systemIndex+1:userIndex], "\n")
		user := strings.Join(lines[userIndex+1:], "\n")
		system = strings.TrimSpace(system)
		user = strings.TrimSpace(user)
		if system == "" || user == "" {
			return "", "", false
		}
		return system, user, true
	}

	return "", "", false
}

func hasRoleTemplateHeader(template string) bool {
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		normalized := strings.ToLower(strings.ReplaceAll(trimmed, "：", ":"))
		if normalized == "system:" || normalized == "user:" {
			return true
		}
	}
	return false
}

func (e *MemoryExtractor) buildExtractionMessages(promptID string, messages []ChatMessage, existingMemories []Memory) []client.Message {
	var existing strings.Builder
	if len(existingMemories) > 0 {
		for i, m := range existingMemories {
			if i > 0 {
				existing.WriteString("\n")
			}
			existing.WriteString(fmt.Sprintf("[%s] (%s/%s) %s", m.ID, m.Subject, m.Category, m.Content))
		}
	} else {
		existing.WriteString("（暂无）")
	}

	var chat strings.Builder
	for i, msg := range messages {
		if i > 0 {
			chat.WriteString("\n")
		}

		role := "用户"
		if msg.Role == "assistant" {
			role = "AI"
		}

		// 清理消息内容，防止 prompt injection
		sanitizedContent := sanitizeMessageContent(msg.Content)
		chat.WriteString(fmt.Sprintf("%s: %s", role, sanitizedContent))
	}

	template := e.loadTemplate()
	systemTemplate, userTemplate, ok := splitRoleTemplate(template)

	userName := e.getUserName()
	avatarName := e.getAvatarName(promptID)
	persona := e.getAvatarPersona(promptID)

	replacer := strings.NewReplacer(
		MemoryExtractionPromptPlaceholderUser, userName,
		MemoryExtractionPromptPlaceholderAvatar, avatarName,
		MemoryExtractionPromptPlaceholderPersona, persona,
		MemoryExtractionPromptPlaceholderExistingMemories, existing.String(),
		MemoryExtractionPromptPlaceholderChatContent, chat.String(),
	)

	if ok {
		systemContent := strings.TrimSpace(replacer.Replace(systemTemplate))
		userContent := strings.TrimSpace(replacer.Replace(userTemplate))
		if systemContent != "" && userContent != "" {
			return []client.Message{
				{Role: "system", Content: systemContent},
				{Role: "user", Content: userContent},
			}
		}
	}

	promptContent := strings.TrimSpace(replacer.Replace(template))
	return []client.Message{{Role: "user", Content: promptContent}}
}

func (e *MemoryExtractor) ExtractAndSave(promptID, sessionID string) error {
	cfg := e.configMgr.Get()
	if !cfg.MemoryEnabled {
		return nil
	}

	contextRounds := cfg.MemoryExtractionRounds

	provider := cfg.MemoryProvider
	if provider == nil || provider.Type == config.ProviderTypeGeminiImage {
		provider = e.configMgr.GetProvider(cfg.MemoryProviderID)
	}
	if provider == nil || provider.Type == config.ProviderTypeGeminiImage {
		provider = e.configMgr.GetActiveProvider()
	}
	if contextRounds <= 0 {
		contextRounds = ContextRounds
	}
	if contextRounds <= 0 {
		contextRounds = 1
	}

	if provider != nil && provider.ContextMessages > 0 && contextRounds > provider.ContextMessages {
		contextRounds = provider.ContextMessages
	}

	messages := e.chatMgr.GetRecentMessages(sessionID, contextRounds*2)
	if len(messages) == 0 {
		return nil
	}

	existingMemories := e.mm.GetAll(promptID)
	if provider == nil {
		logging.Errorf("memory extraction no provider: prompt=%s session=%s", promptID, sessionID)
		return fmt.Errorf("未找到可用的模型配置")
	}
	if provider.Type == config.ProviderTypeGeminiImage {
		logging.Errorf("memory extraction invalid provider type: prompt=%s type=%s", promptID, provider.Type)
		return fmt.Errorf("记忆提取模型不支持对话")
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		logging.Errorf("memory extraction no API key: prompt=%s provider=%s", promptID, provider.ID)
		return fmt.Errorf("记忆提取模型未配置 API Key")
	}

	var aiClient client.AIClient
	switch provider.Type {
	case config.ProviderTypeOpenAIResponse:
		aiClient = client.NewResponsesClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeGemini:
		aiClient = client.NewGeminiClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeAnthropic:
		aiClient = client.NewAnthropicClient(provider.BaseURL, provider.APIKey)
	default:
		aiClient = client.NewClient(provider.BaseURL, provider.APIKey)
	}

	temperature := provider.Temperature
	if provider.Type == config.ProviderTypeAnthropic {
		temperature = 1
	}

	chatReq := client.ChatRequest{
		Model:       provider.Model,
		Messages:    e.buildExtractionMessages(promptID, messages, existingMemories),
		Stream:      false,
		Temperature: temperature,
		TopP:        provider.TopP,
	}

	switch provider.Type {
	case config.ProviderTypeAnthropic:
		chatReq.ThinkingBudget = provider.ThinkingBudget
	case config.ProviderTypeGemini:
		geminiMode := "none"
		geminiLevel := "low"
		geminiBudget := 128
		if provider.GeminiThinkingMode != nil {
			geminiMode = *provider.GeminiThinkingMode
		}
		if provider.GeminiThinkingLevel != nil {
			geminiLevel = *provider.GeminiThinkingLevel
		}
		if provider.GeminiThinkingBudget != nil {
			geminiBudget = *provider.GeminiThinkingBudget
		}
		chatReq.GeminiThinkingMode = geminiMode
		chatReq.GeminiThinkingLevel = geminiLevel
		chatReq.GeminiThinkingBudget = geminiBudget
	default:
		chatReq.ReasoningEffort = provider.ReasoningEffort
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logging.Infof(
		"memory extraction request: prompt=%s session=%s provider=%s model=%s rounds=%d",
		promptID,
		sessionID,
		provider.ID,
		provider.Model,
		contextRounds,
	)

	resp, errChat := aiClient.Chat(ctx, chatReq)
	if errChat != nil {
		logging.Errorf("memory extraction AI request failed: prompt=%s session=%s err=%v", promptID, sessionID, errChat)
		return fmt.Errorf("记忆提取失败: %w", errChat)
	}
	if resp == nil || len(resp.Choices) == 0 {
		logging.Warnf("memory extraction empty response: prompt=%s session=%s", promptID, sessionID)
		return fmt.Errorf("记忆提取失败: 空响应")
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)
	if raw == "" {
		return nil
	}

	var extracted []ExtractedMemory
	errUnmarshal := json.Unmarshal([]byte(raw), &extracted)
	if errUnmarshal != nil {
		logging.Warnf("memory extraction JSON parse failed, trying cleanup: prompt=%s raw=%s err=%v", promptID, logging.Truncate(raw, 300), errUnmarshal)
		cleaned := cleanJSONResponse(raw)
		errUnmarshal = json.Unmarshal([]byte(cleaned), &extracted)
		if errUnmarshal != nil {
			logging.Errorf("memory extraction JSON parse failed after cleanup: prompt=%s cleaned=%s err=%v", promptID, logging.Truncate(cleaned, 300), errUnmarshal)
			return fmt.Errorf("解析记忆提取结果失败: %w", errUnmarshal)
		}
	}

	now := time.Now()
	addedCount := 0
	updatedCount := 0
	invalidCount := 0
	addFailedCount := 0
	updateFailedCount := 0
	for _, item := range extracted {
		subject, category, content, ok := NormalizeExtractedMemoryFields(item.Subject, item.Category, item.Content)
		if !ok {
			invalidCount++
			logging.Warnf(
				"memory extraction field invalid: prompt=%s subject=%s category=%s content=%s",
				promptID,
				item.Subject,
				item.Category,
				logging.Truncate(item.Content, 50),
			)
			continue
		}

		if item.MatchingID != nil && strings.TrimSpace(*item.MatchingID) != "" {
			matchingID := strings.TrimSpace(*item.MatchingID)
			old := e.mm.FindByID(promptID, matchingID)
			if old != nil {
				seenCount := old.SeenCount + 1
				if seenCount <= 0 {
					seenCount = 1
				}
				strength := clamp01(old.Strength)
				strength = math.Min(1.0, strength*1.2+0.15)
				stability := old.Stability
				if stability <= 0 {
					stability = DefaultStabilityForCategory(category)
				}
				stability = math.Min(10.0, stability+0.3)
				errUpdate := e.mm.Patch(promptID, MemoryPatch{
					ID:        matchingID,
					Subject:   &subject,
					Category:  &category,
					Content:   &content,
					Strength:  &strength,
					Stability: &stability,
					LastSeen:  &now,
					SeenCount: &seenCount,
				})
				if errUpdate != nil {
					updateFailedCount++
					logging.Errorf("memory update failed: prompt=%s id=%s err=%v", promptID, matchingID, errUpdate)
				} else {
					updatedCount++
				}
				continue
			}
			logging.Infof("memory matching id not found, adding: prompt=%s id=%s", promptID, matchingID)
		}

		errAdd := e.mm.Add(promptID, Memory{
			Subject:   subject,
			Category:  category,
			Content:   content,
			Strength:  DefaultStrengthForCategory(category),
			SeenCount: 1,
		})
		if errAdd != nil {
			addFailedCount++
			logging.Errorf("memory add failed: prompt=%s err=%v", promptID, errAdd)
		} else {
			addedCount++
		}
	}

	logging.Infof(
		"记忆提取完成 prompt=%s session=%s items=%d added=%d updated=%d invalid=%d add_failed=%d update_failed=%d",
		promptID,
		sessionID,
		len(extracted),
		addedCount,
		updatedCount,
		invalidCount,
		addFailedCount,
		updateFailedCount,
	)
	return nil
}

func cleanJSONResponse(s string) string {
	cleaned := strings.TrimSpace(s)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start >= 0 && end >= 0 && end >= start {
		cleaned = cleaned[start : end+1]
	}

	return strings.TrimSpace(cleaned)
}
