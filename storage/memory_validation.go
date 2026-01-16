package storage

import "strings"

func IsValidMemorySubject(subject string) bool {
	return subject == SubjectUser || subject == SubjectSelf
}

func IsValidMemoryCategory(subject, category string) bool {
	switch subject {
	case SubjectUser:
		switch category {
		case CategoryIdentity, CategoryRelation, CategoryFact, CategoryPreference, CategoryEvent, CategoryEmotion:
			return true
		default:
			return false
		}
	case SubjectSelf:
		switch category {
		case CategoryPromise, CategoryPlan, CategoryStatement, CategoryOpinion:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func NormalizeMemoryContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "--------------------", "—")
	content = strings.ReplaceAll(content, "---", "—")
	content = strings.ReplaceAll(content, "\r\n", " ")
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", " ")
	content = strings.ReplaceAll(content, "\t", " ")
	content = strings.Join(strings.Fields(content), " ")
	content = strings.TrimSpace(content)

	content = strings.TrimPrefix(content, "- ")
	content = strings.TrimPrefix(content, "-")
	content = strings.TrimPrefix(content, "• ")
	content = strings.TrimPrefix(content, "•")
	content = strings.TrimSpace(content)
	return content
}

func TruncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return ""
	}

	runeCount := 0
	for i := range s {
		if runeCount == maxRunes {
			return s[:i]
		}
		runeCount++
	}
	return s
}

func NormalizeExtractedMemoryFields(subject, category, content string) (string, string, string, bool) {
	subject = strings.ToLower(strings.TrimSpace(subject))
	category = strings.ToLower(strings.TrimSpace(category))
	content = NormalizeMemoryContent(content)
	if subject == "" || category == "" || content == "" {
		return "", "", "", false
	}
	if !IsValidMemorySubject(subject) || !IsValidMemoryCategory(subject, category) {
		return "", "", "", false
	}

	content = strings.TrimSpace(TruncateRunes(content, MaxMemoryContentRunes))
	if content == "" {
		return "", "", "", false
	}

	return subject, category, content, true
}
