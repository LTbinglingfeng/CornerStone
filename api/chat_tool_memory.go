package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"cornerstone/storage"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const writeMemoryMaxItems = 3

var (
	writeMemoryPhonePattern               = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`)
	writeMemoryIDCardPattern              = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	writeMemoryBankCardPattern            = regexp.MustCompile(`(?:\d[ -]?){16,19}`)
	writeMemoryCredentialTokenPattern     = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9]{10,}|gh[pousr]_[a-z0-9_]{12,}|xox[baprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z\-_]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})\b`)
	writeMemoryVerificationKeywordPattern = regexp.MustCompile(`(?i)(验证码|校验码|动态码|短信码|otp|passcode|verification code|auth code|one[- ]?time password)`)
	writeMemoryVerificationValuePattern   = regexp.MustCompile(`\b[A-Za-z0-9]{4,8}\b`)
	writeMemoryAddressPattern             = regexp.MustCompile(`(?i)(详细地址|家庭住址|住址|address).{0,24}(?:\d{1,4}|路|街|道|号|室|栋|单元|road|street|avenue|lane|building|room)`)
	writeMemoryCoordinatePattern          = regexp.MustCompile(`-?\d{1,3}\.\d+\s*[,，]\s*-?\d{1,3}\.\d+`)
	writeMemoryCoordinateKeywordPattern   = regexp.MustCompile(`(?i)(经纬度|坐标|latitude|longitude|精确定位|定位位置)`)
	writeMemoryInstructionPattern         = regexp.MustCompile(`(?i)(system prompt|system message|developer message|developer instruction|提示词|系统提示|系统消息|越狱|jailbreak|ignore (?:all )?previous|role\s*:\s*system|请无视|遵循以下指令|始终回复|必须回复|you are chatgpt|你是chatgpt)`)
)

type chatToolWriteMemoryArgs struct {
	Items []chatToolWriteMemoryItem `json:"items"`
}

type chatToolWriteMemoryItem struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

type writeMemoryToolItem struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

type writeMemoryToolData struct {
	PromptID     string                `json:"prompt_id"`
	Added        int                   `json:"added"`
	Updated      int                   `json:"updated"`
	Skipped      int                   `json:"skipped"`
	Blocked      int                   `json:"blocked"`
	Items        []writeMemoryToolItem `json:"items,omitempty"`
	WrittenItems []writeMemoryToolItem `json:"written_items,omitempty"`
}

func (e *chatToolExecutor) handleWriteMemory(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	_ = ctx

	if strings.TrimSpace(toolCtx.PromptID) == "" || toolCtx.MemSession == nil || e.memoryManager == nil {
		return chatToolResult{
			OK:    false,
			Data:  nil,
			Error: "缺少人设/记忆未开启，当前无法写入长期记忆。请不要重试此工具。",
		}
	}

	var args chatToolWriteMemoryArgs
	if err := decodeToolArguments(toolCall.Function.Arguments, &args); err != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}
	if len(args.Items) < 1 || len(args.Items) > writeMemoryMaxItems {
		return chatToolResult{OK: false, Data: nil, Error: "items must contain 1 to 3 entries"}
	}

	data := writeMemoryToolData{
		PromptID: strings.TrimSpace(toolCtx.PromptID),
		Items:    make([]writeMemoryToolItem, 0, len(args.Items)),
	}

	existingByKey := make(map[string]storage.Memory)
	for _, memory := range e.memoryManager.GetAll(toolCtx.PromptID) {
		key := writeMemoryExactKey(memory.Subject, memory.Category, memory.Content)
		if _, exists := existingByKey[key]; !exists {
			existingByKey[key] = memory
		}
	}

	seenRequestKeys := make(map[string]struct{}, len(args.Items))
	now := time.Now()
	for _, input := range args.Items {
		subject, category, content, ok := storage.NormalizeExtractedMemoryFields(input.Subject, input.Category, input.Content)
		if !ok {
			data.Skipped++
			data.Items = append(data.Items, writeMemoryToolItem{
				Subject:  strings.TrimSpace(strings.ToLower(input.Subject)),
				Category: strings.TrimSpace(strings.ToLower(input.Category)),
				Content:  strings.TrimSpace(storage.NormalizeMemoryContent(input.Content)),
				Status:   "skipped",
				Reason:   "subject/category/content 无效",
			})
			continue
		}

		item := writeMemoryToolItem{
			Subject:  subject,
			Category: category,
			Content:  content,
		}

		if reason := detectWriteMemoryBlockReason(content); reason != "" {
			item.Status = "blocked"
			item.Reason = reason
			data.Blocked++
			data.Items = append(data.Items, item)
			continue
		}

		exactKey := writeMemoryExactKey(subject, category, content)
		if _, exists := seenRequestKeys[exactKey]; exists {
			item.Status = "skipped"
			item.Reason = "本次调用中重复的记忆条目"
			data.Skipped++
			data.Items = append(data.Items, item)
			continue
		}
		seenRequestKeys[exactKey] = struct{}{}

		if existing, exists := existingByKey[exactKey]; exists {
			updated := existing
			updated.Subject = subject
			updated.Category = category
			updated.Content = content
			updated.ReinforceAt(now)
			errUpdate := e.memoryManager.Patch(toolCtx.PromptID, storage.MemoryPatch{
				ID:        existing.ID,
				Subject:   &subject,
				Category:  &category,
				Content:   &content,
				Strength:  &updated.Strength,
				Stability: &updated.Stability,
				LastSeen:  &updated.LastSeen,
				SeenCount: &updated.SeenCount,
			})
			if errUpdate != nil {
				logging.Errorf("write_memory update failed: prompt=%s id=%s err=%v", toolCtx.PromptID, existing.ID, errUpdate)
				item.Status = "skipped"
				item.Reason = "更新记忆失败"
				data.Skipped++
				data.Items = append(data.Items, item)
				continue
			}

			existingByKey[exactKey] = updated
			item.Status = "updated"
			data.Updated++
			data.Items = append(data.Items, item)
			data.WrittenItems = append(data.WrittenItems, item)
			continue
		}

		errAdd := e.memoryManager.Add(toolCtx.PromptID, storage.Memory{
			Subject:   subject,
			Category:  category,
			Content:   content,
			Strength:  storage.DefaultStrengthForCategory(category),
			Stability: storage.DefaultStabilityForCategory(category),
			LastSeen:  now,
			SeenCount: 1,
			CreatedAt: now,
		})
		if errAdd != nil {
			logging.Errorf("write_memory add failed: prompt=%s err=%v", toolCtx.PromptID, errAdd)
			item.Status = "skipped"
			item.Reason = "写入记忆失败"
			data.Skipped++
			data.Items = append(data.Items, item)
			continue
		}

		item.Status = "added"
		data.Added++
		data.Items = append(data.Items, item)
		data.WrittenItems = append(data.WrittenItems, item)
	}

	if data.Added > 0 || data.Updated > 0 {
		toolCtx.MemSession.RefreshNow()
	}

	return chatToolResult{
		OK:   true,
		Data: data,
	}
}

func writeMemoryExactKey(subject, category, content string) string {
	return subject + "\x00" + category + "\x00" + content
}

func detectWriteMemoryBlockReason(content string) string {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return ""
	}
	lower := strings.ToLower(normalized)

	if strings.Contains(lower, "password") || strings.Contains(normalized, "密码") || strings.Contains(normalized, "口令") {
		return "包含密码或口令信息"
	}
	if writeMemoryCredentialTokenPattern.MatchString(normalized) {
		return "包含疑似 API Key 或 Token"
	}
	if containsCredentialKeyword(lower) && containsSecretLikeSegment(normalized) {
		return "包含疑似 API Key 或 Token"
	}
	if writeMemoryPhonePattern.MatchString(normalized) {
		return "包含手机号"
	}
	if writeMemoryIDCardPattern.MatchString(normalized) {
		return "包含身份证号"
	}
	if writeMemoryBankCardPattern.MatchString(normalized) {
		return "包含银行卡号"
	}
	if writeMemoryVerificationKeywordPattern.MatchString(normalized) && writeMemoryVerificationValuePattern.MatchString(normalized) {
		return "包含验证码或一次性口令"
	}
	if writeMemoryAddressPattern.MatchString(normalized) || writeMemoryCoordinatePattern.MatchString(normalized) {
		return "包含详细地址或精确定位信息"
	}
	if writeMemoryCoordinateKeywordPattern.MatchString(normalized) && strings.IndexFunc(normalized, unicode.IsDigit) >= 0 {
		return "包含详细地址或精确定位信息"
	}
	if writeMemoryInstructionPattern.MatchString(normalized) {
		return "包含指令或提示词内容，不适合作为长期记忆"
	}
	return ""
}

func containsCredentialKeyword(lower string) bool {
	for _, keyword := range []string{
		"api key",
		"apikey",
		"access token",
		"refresh token",
		"token",
		"secret",
		"secret key",
		"密钥",
		"令牌",
		"凭证",
		"私钥",
	} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func containsSecretLikeSegment(content string) bool {
	fields := strings.FieldsFunc(content, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ',', '，', '.', '。', ':', '：', ';', '；', '"', '\'', '(', ')', '[', ']', '{', '}', '<', '>':
			return true
		default:
			return false
		}
	})

	for _, field := range fields {
		if isSecretLikeSegment(field) {
			return true
		}
	}
	return false
}

func isSecretLikeSegment(segment string) bool {
	trimmed := strings.TrimSpace(segment)
	if len(trimmed) < 12 || len(trimmed) > 256 {
		return false
	}

	hasLetter := false
	hasDigit := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case strings.ContainsRune("-_=/+.:", r):
		default:
			return false
		}
	}

	if !hasLetter {
		return false
	}
	return hasDigit || strings.ContainsAny(trimmed, "-_=/+")
}
