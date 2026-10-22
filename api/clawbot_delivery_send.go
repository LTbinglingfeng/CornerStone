package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *ClawBotService) deliverChunk(ctx context.Context, cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob) {
	if ctx.Err() != nil {
		return
	}
	valid, originErr := s.validateDeliveryOrigin(job)
	if originErr != nil {
		s.parkDelivery(cfg, job.ID, "send", "Could not validate the source task before sending.", false)
		return
	}
	if !valid {
		s.cancelObsoleteDelivery(cfg, job.ID)
		return
	}
	if job.NextChunk >= len(job.Chunks) {
		s.parkDelivery(cfg, job.ID, "send", "Invalid chunk cursor", false)
		return
	}
	if media := job.Chunks[job.NextChunk].Media; media != nil && media.Uploaded == nil {
		s.uploadDeliveryChunk(ctx, cfg, job)
		return
	}
	err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		if j.Status != "ready" || j.NextChunk != job.NextChunk || j.Revision != job.Revision {
			return errClawBotJobUnavailable
		}
		j.Status = "sending"
		j.Chunks[j.NextChunk].Status = "sending"
		j.Attempts++
		return nil
	})
	if err != nil {
		if !errors.Is(err, errClawBotJobUnavailable) {
			s.deliveryFault(err)
		}
		return
	}
	chunk := job.Chunks[job.NextChunk]
	item := clawBotDeliveryWireItem(chunk)
	response, err := s.client.SendItemMessageWithID(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, job.UserID, s.getContextToken(job.UserID), item, chunk.ClientID)
	if err != nil {
		var deliveryErr *client.ClawBotDeliveryError
		if errors.As(err, &deliveryErr) && deliveryErr.Outcome != client.ClawBotDeliveryUncertain {
			if deliveryErr.Retryable || client.IsClawBotStaleToken(err) || errors.Is(err, context.Canceled) {
				saveErr := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
					j.Status = "ready"
					j.Chunks[j.NextChunk].Status = "pending"
					j.LastError = err.Error()
					j.NextAttemptAt = time.Now().Add(clawBotRetryDelay(j.Attempts))
					return nil
				})
				if saveErr != nil {
					s.deliveryFault(saveErr)
				}
				if client.IsClawBotStaleToken(err) {
					s.markTokenStale(cfg.BotToken)
				}
			} else {
				s.parkDelivery(cfg, job.ID, "send", err.Error(), false)
			}
		} else {
			s.parkDelivery(cfg, job.ID, "send", "Send result is unknown ("+err.Error()+"); treated as delivered.", true)
		}
		return
	}
	if err = s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		j.Chunks[j.NextChunk].Status = "sent"
		j.Chunks[j.NextChunk].MessageID = string(response.MessageID)
		j.NextChunk++
		j.Status = "committing"
		j.Phase = "history"
		j.LastError = ""
		j.NextAttemptAt = time.Time{}
		j.Attempts = 0
		return nil
	}); err != nil {
		s.deliveryFault(err)
		return
	}
	protocolSession := ""
	if len(job.Inputs) > 0 {
		protocolSession = job.Inputs[len(job.Inputs)-1].SessionID
	}
	s.cacheClawBotReference(cfg, client.ClawBotIncomingMessage{MessageID: response.MessageID, FromUserID: job.UserID, SessionID: protocolSession, ItemList: []client.ClawBotIncomingMessageItem{item}})
	if media := chunk.Media; media != nil {
		localPath := media.HistoryPath
		if localPath == "" {
			localPath = media.SourcePath
		}
		s.recordClawBotSentMediaReference(cfg, job.UserID, protocolSession, response.MessageID, localPath, media.Name, media.Kind)
	}
}

func confirmedDeliveryMessages(job storage.ClawBotDeliveryJob) []storage.ChatMessage {
	for _, chunk := range job.Chunks {
		if chunk.Media != nil {
			return confirmedClawBotMediaMessages(job)
		}
	}
	if job.NextChunk >= len(job.Chunks) {
		return job.Messages
	}
	// The transcript contains only confirmed text. Preserve the complete tool and
	// reasoning payload, replacing the final visible assistant text with its sent prefix.
	messages := append([]storage.ChatMessage(nil), job.Messages...)
	texts := make([]string, 0, job.NextChunk)
	for i := 0; i < job.NextChunk; i++ {
		texts = append(texts, job.Chunks[i].Text)
	}
	text := strings.Join(texts, "\n")
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) == 0 && strings.TrimSpace(messages[i].Content) != "" {
			messages[i].ProviderState = nil
			messages[i].Content = text
			token := ""
			messages[i].AssistantMessageSplitToken = &token
			if shouldDropEmptyAssistantClientMessage(client.Message{Role: messages[i].Role, Content: messages[i].Content, ReasoningContent: messages[i].ReasoningContent, ProviderState: messages[i].ProviderState, ToolCalls: messages[i].ToolCalls, ToolCallID: messages[i].ToolCallID, ImagePaths: messages[i].ImagePaths, TTSAudioPaths: messages[i].TTSAudioPaths}) {
				messages = append(messages[:i], messages[i+1:]...)
			}
			return messages
		}
	}
	if text != "" {
		messages = append(messages, storage.ChatMessage{Role: "assistant", Content: text, Timestamp: job.CreatedAt})
	}
	return messages
}

func (s *ClawBotService) commitDelivery(cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob) {
	messages := confirmedDeliveryMessages(job)
	historyAbandoned := false
	// Cancellation stops unsent chunks, not reconciliation of acknowledged ones.
	if job.SessionID != "" && !job.HistoryCommitted && (!job.CancelRequested || job.NextChunk > job.HistoryChunks || len(messages) > 0) {
		operationID := fmt.Sprintf("wx-out:%s:%d", job.ID, job.NextChunk)
		var err error
		if job.HistoryChunks > 0 {
			err = s.handler.chatManager.CommitDeliverySuffix(job.SessionID, operationID, job.HistoryMessages, messages)
		} else {
			err = s.handler.chatManager.CommitDeliveryBatch(job.SessionID, operationID, messages, job.Replace, job.ExpectedTail)
		}
		if err != nil {
			if !job.CancelRequested {
				s.parkDelivery(cfg, job.ID, "history", "Confirmed message history could not be committed (storage error, deleted session or concurrent edit). Resolve the conflict before retrying; confirmed segments will not be sent again.", false)
				return
			}
			// Explicit cancellation may release ordering after a deleted/edited
			// transcript. Keep the full audit indefinitely, never pretend it saved.
			historyAbandoned = true
		}
	}
	complete := job.CancelRequested || job.NextChunk >= len(job.Chunks)
	if err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		if j.Status != "committing" || j.NextChunk != job.NextChunk || j.Revision != job.Revision {
			return errClawBotJobUnavailable
		}
		if !historyAbandoned {
			j.HistoryChunks = j.NextChunk
			j.HistoryMessages = messages
		}
		j.HistoryCommitted = complete && !historyAbandoned
		if !complete {
			j.Status = "ready"
			j.Phase = "send"
		}
		return nil
	}); err != nil {
		if !errors.Is(err, errClawBotJobUnavailable) {
			s.deliveryFault(err)
		}
		return
	}
	if !complete {
		return
	}
	// A successful queue handoff is not a sent reminder. Finalize its producer
	// only after every segment and its history receipt are durable.
	if err := s.finishDeliveryOrigin(job, job.CancelRequested); err != nil {
		s.parkDelivery(cfg, job.ID, "history", "Could not finalize the reminder/greeting record; retry will not resend messages.", false)
		return
	}
	memoryPrompt := ""
	if err := s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		j := deliveryJob(d, job.ID)
		if j == nil {
			return errClawBotJobUnavailable
		}
		if !j.CancelRequested && !j.MemoryNotified && j.MemoryPromptID != "" {
			// At-most-once dispatch: a crash after this marker must not replay a
			// potentially side-effecting memory extraction.
			j.MemoryNotified = true
			memoryPrompt = j.MemoryPromptID
		}
		j.Revision++
		j.Status = "done"
		if j.CancelRequested {
			j.Status = "cancelled"
		}
		j.LastError = ""
		if historyAbandoned {
			j.LastError = "Cancelled with uncommitted confirmed history. The delivery audit is retained; retry reconciles history only and will not resend."
		}
		j.UpdatedAt = time.Now()
		d.Finished[j.ID] = j.Status
		return nil
	}); err != nil {
		s.deliveryFault(err)
		return
	}
	if memoryPrompt != "" {
		if session := s.handler.getOrCreateMemorySession(memoryPrompt, job.SessionID); session != nil {
			session.OnRoundComplete()
		}
	}
}

func (s *ClawBotService) cancelObsoleteDelivery(cfg config.ClawBotConfig, id string) {
	if err := s.changeDelivery(cfg, id, func(j *storage.ClawBotDeliveryJob) error {
		j.CancelRequested = true
		j.Status = "committing"
		j.Phase = "history"
		j.LastError = "Source task was cancelled, superseded or is no longer eligible"
		return nil
	}); err != nil {
		s.deliveryFault(err)
	}
}
