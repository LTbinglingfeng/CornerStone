package storage

import (
	"fmt"
	"strings"
)

func BuildPromptWithMemory(basePrompt string, memories []Memory) string {
	if len(memories) == 0 {
		return basePrompt
	}

	userMems := make([]Memory, 0, len(memories))
	selfMems := make([]Memory, 0, len(memories))
	for _, m := range memories {
		if m.Subject == SubjectUser {
			userMems = append(userMems, m)
		} else {
			selfMems = append(selfMems, m)
		}
	}

	var block strings.Builder
	block.WriteString("\n\n---\n")

	if len(userMems) > 0 {
		block.WriteString("## 你对用户的了解：\n")
		for _, m := range userMems {
			block.WriteString(fmt.Sprintf("- %s\n", m.Content))
		}
	}

	if len(selfMems) > 0 {
		if len(userMems) > 0 {
			block.WriteString("\n")
		}
		block.WriteString("## 你说过的话/做过的承诺：\n")
		for _, m := range selfMems {
			block.WriteString(fmt.Sprintf("- %s\n", m.Content))
		}
	}

	block.WriteString("\n请自然运用这些记忆，信守承诺，不要刻意复述。\n---\n")
	return basePrompt + block.String()
}
