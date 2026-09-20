package api

import (
	"cornerstone/client"
	"cornerstone/storage"
	"strings"
	"time"
)

// Reconstruct visible attachment metadata from acknowledgements only. Tool call
// and result payloads remain intact as an execution audit (not a send receipt).
func confirmedClawBotMediaMessages(job storage.ClawBotDeliveryJob) []storage.ChatMessage {
	messages := append([]storage.ChatMessage(nil), job.Messages...)
	final := -1
	for i := range messages {
		m := &messages[i]
		if m.Role != "assistant" && m.Role != "tool" {
			continue
		}
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" {
			final = i
		}
		m.ImagePaths = nil
		m.TTSAudioPaths = nil
		m.Attachments = nil
	}
	texts := []string{}
	receipts := make(map[int][]storage.ChatMessage)
	for i := 0; i < job.NextChunk && i < len(job.Chunks); i++ {
		c := job.Chunks[i]
		if c.Media == nil {
			texts = append(texts, c.Text)
			continue
		}
		if c.MessageIndex == nil || *c.MessageIndex < 0 || *c.MessageIndex >= len(messages) {
			continue
		}
		index := *c.MessageIndex
		m := &messages[index]
		var receipt storage.ChatMessage
		if m.Role == "tool" {
			receipt = storage.ChatMessage{Role: "assistant", Timestamp: job.CreatedAt.Add(time.Duration(i+1) * time.Millisecond)}
			m = &receipt
		}
		media := c.Media
		mediaPath := media.HistoryPath
		if mediaPath == "" {
			mediaPath = media.SourcePath
		}
		if media.Kind == "image" {
			m.ImagePaths = append(m.ImagePaths, mediaPath)
		} else {
			if strings.HasPrefix(media.SourcePath, "tts_audio/") {
				m.TTSAudioPaths = append(m.TTSAudioPaths, media.SourcePath)
			}
			if !strings.HasPrefix(mediaPath, "tts_audio/") {
				m.Attachments = append(m.Attachments, storage.ChatAttachment{Path: mediaPath, Name: media.Name, MIMEType: media.MIMEType, Kind: media.Kind, Size: media.Size})
			}
		}
		if receipt.Role != "" {
			receipts[index] = append(receipts[index], receipt)
		}
	}
	if final >= 0 {
		if job.NextChunk < len(job.Chunks) {
			messages[final].Content = strings.Join(texts, "\n")
			token := ""
			messages[final].AssistantMessageSplitToken = &token
		}
	} else if len(texts) > 0 {
		messages = append(messages, storage.ChatMessage{Role: "assistant", Content: strings.Join(texts, "\n"), Timestamp: job.CreatedAt})
	}
	filtered := make([]storage.ChatMessage, 0, len(messages))
	var pendingReceipts []storage.ChatMessage
	for index, m := range messages {
		if len(m.Attachments) == 0 && shouldDropEmptyAssistantClientMessage(client.Message{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, ImagePaths: m.ImagePaths, TTSAudioPaths: m.TTSAudioPaths}) {
			continue
		}
		filtered = append(filtered, m)
		pendingReceipts = append(pendingReceipts, receipts[index]...)
		// Keep each tool-call group's results contiguous for provider replay.
		// Receipts remain in delivery order, but follow the entire result group.
		if index+1 == len(messages) || messages[index+1].Role != "tool" {
			filtered = append(filtered, pendingReceipts...)
			pendingReceipts = nil
		}
	}
	return filtered
}
