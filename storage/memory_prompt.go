package storage

import (
	"fmt"
	"strings"
)

func BuildPromptWithMemory(basePrompt string, memories []Memory) string {
	if len(memories) == 0 {
		return basePrompt
	}

	userMems := make([]string, 0, len(memories))
	selfMems := make([]string, 0, len(memories))
	for _, m := range memories {
		content := strings.TrimSpace(TruncateRunes(NormalizeMemoryContent(m.Content), MaxMemoryContentRunes))
		if content == "" {
			continue
		}
		switch m.Subject {
		case SubjectUser:
			userMems = append(userMems, content)
		case SubjectSelf:
			selfMems = append(selfMems, content)
		default:
			continue
		}
	}

	if len(userMems) == 0 && len(selfMems) == 0 {
		return basePrompt
	}

	var block strings.Builder
	block.WriteString("\n\n---\n")

	if len(userMems) > 0 {
		block.WriteString("## 你对用户的了解（每条不超过100字）：\n")
		for _, content := range userMems {
			block.WriteString(fmt.Sprintf("- %s\n", content))
		}
	}

	if len(selfMems) > 0 {
		if len(userMems) > 0 {
			block.WriteString("\n")
		}
		block.WriteString("## 你说过的话/做过的承诺（每条不超过100字）：\n")
		for _, content := range selfMems {
			block.WriteString(fmt.Sprintf("- %s\n", content))
		}
	}

	block.WriteString("\n请自然运用这些记忆，信守承诺，不要刻意复述。\n---\n")
	return basePrompt + block.String()
}
