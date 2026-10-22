package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"cornerstone/storage"
	"strings"
	"time"
)

type chatToolScheduleReminderArgs struct {
	DueAt          string `json:"due_at"`
	Title          string `json:"title"`
	ReminderPrompt string `json:"reminder_prompt"`
}

func (e *chatToolExecutor) handleScheduleReminder(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	_ = ctx
	if toolCtx.Channel == chatToolChannelQQOfficial {
		return chatToolResult{Error: "reminders unavailable on QQ Official"}
	}

	if e.reminderService == nil {
		return chatToolResult{OK: false, Data: nil, Error: "reminder service not configured"}
	}

	var args chatToolScheduleReminderArgs
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}

	dueAt, errParse := parseReminderDueAt(args.DueAt)
	if errParse != nil {
		return chatToolResult{OK: false, Data: nil, Error: "due_at must be a valid RFC3339 time with timezone"}
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		return chatToolResult{OK: false, Data: nil, Error: "title is required"}
	}

	reminderPrompt := strings.TrimSpace(args.ReminderPrompt)
	if reminderPrompt == "" {
		return chatToolResult{OK: false, Data: nil, Error: "reminder_prompt is required"}
	}

	sessionID := strings.TrimSpace(toolCtx.SessionID)
	if sessionID == "" {
		return chatToolResult{OK: false, Data: nil, Error: "missing session context"}
	}

	promptID := strings.TrimSpace(toolCtx.PromptID)
	if promptID == "" {
		return chatToolResult{OK: false, Data: nil, Error: "missing prompt context"}
	}

	channel := storage.ReminderChannelWeb
	target := toolCtx.Target
	if toolCtx.Channel == chatToolChannelClawBot {
		channel = storage.ReminderChannelClawBot
		if strings.TrimSpace(target.UserID) == "" {
			return chatToolResult{OK: false, Data: nil, Error: "missing clawbot user context"}
		}
	} else if toolCtx.Channel == chatToolChannelNapCat {
		channel = storage.ReminderChannelNapCat
		if strings.TrimSpace(target.UserID) == "" || strings.TrimSpace(target.BotSelfID) == "" {
			return chatToolResult{OK: false, Data: nil, Error: "missing napcat private context"}
		}
	}

	created, errCreate := e.reminderService.Create(reminderCreateRequest{
		Channel:        channel,
		SessionID:      sessionID,
		PromptID:       promptID,
		PromptName:     strings.TrimSpace(toolCtx.PromptName),
		Target:         target,
		Title:          title,
		ReminderPrompt: reminderPrompt,
		DueAt:          dueAt,
	})
	if errCreate != nil {
		logging.Errorf("schedule reminder create failed: channel=%s session=%s prompt=%s err=%v", channel, sessionID, promptID, errCreate)
		return chatToolResult{OK: false, Data: nil, Error: errCreate.Error()}
	}

	return chatToolResult{
		OK: true,
		Data: map[string]interface{}{
			"id":     created.ID,
			"title":  created.Title,
			"due_at": created.DueAt.Format(time.RFC3339),
			"status": created.Status,
		},
	}
}
