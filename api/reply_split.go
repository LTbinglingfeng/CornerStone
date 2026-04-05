package api

import "strings"

func splitTextByMaxRunes(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return nil
	}

	remaining := []rune(text)
	chunks := make([]string, 0, 4)
	for len(remaining) > 0 {
		if len(remaining) <= maxRunes {
			chunk := strings.TrimSpace(string(remaining))
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			break
		}

		cut := maxRunes
		for i := maxRunes; i > 0; i-- {
			if remaining[i-1] == '\n' {
				cut = i
				break
			}
		}

		chunk := strings.TrimSpace(string(remaining[:cut]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = remaining[cut:]
		for len(remaining) > 0 && (remaining[0] == '\n' || remaining[0] == '\r' || remaining[0] == ' ') {
			remaining = remaining[1:]
		}
	}
	return chunks
}

func splitAssistantReplyMessages(text string, maxRunes int) []string {
	segments := splitAssistantMessageContent(text)
	if len(segments) == 0 {
		return splitTextByMaxRunes(text, maxRunes)
	}

	messages := make([]string, 0, len(segments))
	for _, segment := range segments {
		chunks := splitTextByMaxRunes(segment, maxRunes)
		if len(chunks) == 0 {
			continue
		}
		messages = append(messages, chunks...)
	}
	if len(messages) == 0 {
		return splitTextByMaxRunes(text, maxRunes)
	}
	return messages
}
