package api

import (
	"context"
	"cornerstone/client"
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
	if toolCtx.Channel == chatToolChannelClawBot {
		channel = storage.ReminderChannelClawBot
		if strings.TrimSpace(toolCtx.ClawBotUserID) == "" {
			return chatToolResult{OK: false, Data: nil, Error: "missing clawbot user context"}
		}
	}

	created, errCreate := e.reminderService.Create(reminderCreateRequest{
		Channel:        channel,
		SessionID:      sessionID,
		PromptID:       promptID,
		PromptName:     strings.TrimSpace(toolCtx.PromptName),
		ClawBotUserID:  strings.TrimSpace(toolCtx.ClawBotUserID),
		Title:          title,
		ReminderPrompt: reminderPrompt,
		DueAt:          dueAt,
	})
	if errCreate != nil {
		return chatToolResult{OK: false, Data: nil, Error: errCreate.Error()}
	}

	return chatToolResult{
		OK: true,
		Data: map[string]interface{}{
			"id":              created.ID,
			"channel":         created.Channel,
			"session_id":      created.SessionID,
			"prompt_id":       created.PromptID,
			"prompt_name":     created.PromptName,
			"title":           created.Title,
			"reminder_prompt": created.ReminderPrompt,
			"due_at":          created.DueAt.Format(time.RFC3339),
			"status":          created.Status,
		},
	}
}
