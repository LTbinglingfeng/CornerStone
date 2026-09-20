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

func (s *ClawBotService) stepDelivery(ctx context.Context, cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob) {
	if ctx.Err() != nil || !s.isCurrentAccount(cfg) {
		return
	}
	cfg = s.handler.configManager.GetClawBotConfig()
	switch job.Status {
	case "ready":
		s.deliverChunk(ctx, cfg, job)
		return
	case "committing":
		s.commitDelivery(cfg, job)
		return
	case "queued":
	default:
		return
	}
	next := "preparing"
	if job.Kind == "command" {
		next = "executing"
	}
	if err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		if j.Status != "queued" || time.Now().Before(j.ReadyAt) || time.Now().Before(j.NextAttemptAt) {
			return errClawBotJobUnavailable
		}
		j.Status = next
		j.LastError = ""
		return nil
	}); err != nil {
		if !errors.Is(err, errClawBotJobUnavailable) {
			s.deliveryFault(err)
		}
		return
	}
	// The scan snapshot can predate a merged input. Execute the payload captured
	// after the atomic claim; queued inputs can no longer be merged now.
	claimed, ok := s.snapshotDelivery(job.ID)
	if !ok {
		return
	}
	job = claimed
	valid, originErr := s.validateDeliveryOrigin(job)
	if originErr != nil {
		s.parkDelivery(cfg, job.ID, "preparation", "Could not validate the source task; retry after resolving its state.", false)
		return
	}
	if !valid {
		s.cancelObsoleteDelivery(cfg, job.ID)
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, clawBotProcessTimeout)
	defer cancel()
	runCtx = context.WithValue(runCtx, clawBotDeliveryContextKey{}, job.ID)
	if job.Kind == "command" {
		if len(job.Inputs) == 0 {
			s.parkDelivery(cfg, job.ID, "command", "Missing command payload", false)
			return
		}
		msg := job.Inputs[0]
		msg.ContextToken = "" // Receiving already committed the latest context atomically.
		command, _ := parseClawBotCommand(client.ExtractTextFromClawBotMessage(msg))
		if command.Name == clawBotCommandRegenerate && strings.TrimSpace(command.Args) == "" && isClawBotCommandAllowed(cfg, command.Name) {
			s.generateDelivery(runCtx, cfg, job, true)
			return
		}
		s.handleIncomingMessage(runCtx, cfg, msg)
		current, ok := s.snapshotDelivery(job.ID)
		if ok && current.Status == "executing" {
			s.parkDelivery(cfg, job.ID, "command", "Command execution did not produce a durable outcome; review before retrying.", true)
		}
		return
	}
	s.generateDelivery(runCtx, cfg, job, false)
}

func (s *ClawBotService) prepareDeliveryInput(ctx context.Context, cfg config.ClawBotConfig, job *storage.ClawBotDeliveryJob) (*storage.ChatRecord, error) {
	if job.SessionID == "" {
		session, err := s.getOrCreateActiveSession(job.UserID, cfg.PromptID)
		if err != nil {
			return nil, err
		}
		job.SessionID = session.SessionID
	}
	// Do not unlink received files on a state-save error: rename may already
	// have committed the queue snapshot before directory fsync failed. Offline
	// orphan collection can safely distinguish unreferenced attachments later.
	if len(job.InputMessages) == 0 {
		for index, msg := range job.Inputs {
			msg = s.restoreClawBotReferences(cfg, msg)
			text := strings.TrimSpace(client.ExtractTextFromClawBotMessage(msg))
			if literal, ok := unescapeClawBotLiteralCommandText(text); ok {
				text = literal
			}
			images := []string(nil)
			for _, item := range client.ExtractImageItemsFromClawBotMessage(msg) {
				data, err := s.client.DownloadImageItem(ctx, item)
				if err != nil {
					text = strings.TrimSpace(text + "\n[图片下载失败，内容不可用。]")
					continue
				}
				path, err := s.handler.saveCachePhotoBytes(data)
				if err != nil {
					text = strings.TrimSpace(text + "\n[图片校验或保存失败，内容不可用。]")
					continue
				}
				images = append(images, path)
			}
			attachments, notice := s.receiveClawBotAttachments(ctx, msg)
			if notice != "" {
				text = strings.TrimSpace(text + "\n" + notice)
			}
			if text == "" && len(images) == 0 && len(attachments) == 0 {
				continue
			}
			at := job.CreatedAt.Add(time.Duration(index) * time.Millisecond)
			if index < len(job.InputTimes) && !job.InputTimes[index].IsZero() {
				at = job.InputTimes[index]
			}
			job.InputMessages = append(job.InputMessages, storage.ChatMessage{Role: "user", Content: text, ImagePaths: images, Attachments: attachments, Timestamp: at})
		}
	}
	if err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		j.SessionID = job.SessionID
		j.InputMessages = job.InputMessages
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.handler.chatManager.CommitDeliveryBatch(job.SessionID, "wx-in:"+job.ID, job.InputMessages, false, nil); err != nil {
		return nil, err
	}
	session, ok := s.handler.chatManager.GetSession(job.SessionID)
	if !ok {
		return nil, fmt.Errorf("session no longer exists")
	}
	// Rebuild an idle task only after the inbound history transaction is durable.
	if s.handler.idleGreetingService != nil && len(job.InputMessages) > 0 {
		_, err := s.handler.idleGreetingService.Rebuild(idleGreetingScheduleRequest{Channel: storage.ReminderChannelClawBot, SessionID: job.SessionID, PromptID: session.PromptID, PromptName: session.PromptName, Target: storage.ReminderTarget{Kind: storage.ReminderTargetKindUser, UserID: job.UserID}, LastUserAt: job.InputMessages[len(job.InputMessages)-1].Timestamp})
		if err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (s *ClawBotService) generateDelivery(ctx context.Context, cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob, regenerate bool) {
	var session *storage.ChatRecord
	var err error
	if regenerate {
		if job.SessionID == "" {
			current, ok := s.getCurrentSessionForUser(job.UserID, cfg.PromptID)
			if !ok {
				s.parkDelivery(cfg, job.ID, "command", "No session to regenerate", false)
				return
			}
			job.SessionID = current.SessionID
		}
		snapshot, snapshotErr := s.handler.chatManager.SnapshotTrailingResponseBatch(job.SessionID)
		if snapshotErr != nil || snapshot == nil || len(snapshot.TailMessages) == 0 {
			s.parkDelivery(cfg, job.ID, "command", "No response batch to regenerate", false)
			return
		}
		session = snapshot.Session
		job.ExpectedTail = snapshot.TailMessages
		job.Replace = true
	} else if job.Kind == "chat" {
		session, err = s.prepareDeliveryInput(ctx, cfg, &job)
	} else {
		var ok bool
		session, ok = s.handler.chatManager.GetSession(job.SessionID)
		if !ok {
			err = fmt.Errorf("session no longer exists")
		}
	}
	if err != nil {
		s.parkDelivery(cfg, job.ID, "preparation", "Preparing input or writing inbound history failed; retry after resolving the error.", false)
		return
	}
	if ctx.Err() != nil {
		return
	} // preparing is safe to resume
	if err = s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		j.Status = "generating"
		j.Phase = "generation"
		j.SessionID = job.SessionID
		j.Replace = job.Replace
		j.ExpectedTail = job.ExpectedTail
		return nil
	}); err != nil {
		s.deliveryFault(err)
		return
	}
	stopTyping := s.startClawBotTyping(ctx, cfg, job.UserID)
	defer stopTyping()
	var result *clawBotGeneratedReply
	switch job.Kind {
	case "reminder":
		if job.Reminder == nil || s.handler.reminderService == nil {
			err = fmt.Errorf("reminder service unavailable")
			break
		}
		prompt, ok := s.handler.promptManager.Get(job.Reminder.PromptID)
		if !ok {
			err = fmt.Errorf("reminder persona no longer exists")
			break
		}
		var reply *generatedSessionReply
		reply, err = s.handler.reminderService.generateReminderReply(ctx, job.Reminder, session, prompt)
		if reply != nil {
			result = &clawBotGeneratedReply{Text: reply.Text, StorageMessages: reply.StorageMessages, MemSession: reply.MemSession}
		}
	case "greeting":
		if job.Greeting == nil || s.handler.idleGreetingService == nil {
			err = fmt.Errorf("greeting service unavailable")
			break
		}
		var reply *generatedSessionReply
		reply, err = s.handler.idleGreetingService.generateReply(ctx, job.Greeting, session, s.handler.configManager.Get())
		if reply != nil {
			result = &clawBotGeneratedReply{Text: reply.Text, StorageMessages: reply.StorageMessages, MemSession: reply.MemSession}
		}
	default:
		result, err = s.generateReplyForSession(ctx, session, cfg.PromptID, job.UserID)
	}
	stopTyping()
	if err != nil {
		s.parkDelivery(cfg, job.ID, "generation", "Reply generation was interrupted or failed. Tools may already have run; review before retrying.", true)
		return
	}
	if result == nil {
		result = &clawBotGeneratedReply{}
	}
	if err = s.stageDeliveryReply(cfg, job.ID, job.SessionID, result, job.Replace, job.ExpectedTail); err != nil {
		s.deliveryFault(err)
	}
}

func (s *ClawBotService) stageDeliveryReply(cfg config.ClawBotConfig, id, sessionID string, reply *clawBotGeneratedReply, replace bool, expected []storage.ChatMessage) error {
	text := strings.TrimSpace(reply.Text)
	messages := reply.StorageMessages
	if len(messages) == 0 && text != "" {
		messages = []storage.ChatMessage{{Role: "assistant", Content: text, Timestamp: time.Now()}}
	}
	token := configuredAssistantMessageSplitToken(s.handler.configManager)
	ensureAssistantMessageSplitToken(messages, token)
	texts := splitClawBotReplyMessages(text, token, clawBotReplyChunkMaxRune)
	return s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		j := deliveryJob(d, id)
		if j == nil {
			return errClawBotJobUnavailable
		}
		if j.Status != "generating" && j.Status != "executing" {
			return errClawBotJobUnavailable
		}
		j.SessionID = sessionID
		j.Messages = messages
		j.Replace = replace
		j.ExpectedTail = expected
		j.NextChunk = 0
		j.Chunks = make([]storage.ClawBotDeliveryChunk, 0, len(texts))
		for index, text := range texts {
			j.Chunks = append(j.Chunks, storage.ClawBotDeliveryChunk{ClientID: fmt.Sprintf("cs-%s-%d", id, index), Text: text, Status: "pending"})
		}
		j.Status = "ready"
		j.Phase = "send"
		j.LastError = ""
		j.UpdatedAt = time.Now()
		j.Revision++
		if reply.MemSession != nil && sessionID != "" {
			j.MemoryPromptID = reply.MemSession.PromptID()
		}
		if len(j.Chunks) == 0 {
			j.Status = "committing"
			j.Phase = "history"
		}
		if session, ok := s.getActiveSessionID(j.UserID); ok {
			d.ActiveSessions[j.UserID] = session
		} else {
			delete(d.ActiveSessions, j.UserID)
		}
		return nil
	})
}
