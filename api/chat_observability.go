package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"cornerstone/storage"
	"time"
)

func chatLogSource(channel chatToolChannel) string {
	if channel == chatToolChannelDefault || channel == "" {
		return "web"
	}
	return string(channel)
}

type webChatLogKey struct{}

type webChatLog struct {
	ctx       context.Context
	started   time.Time
	status    string
	history   string
	delivery  string
	errorKind string
	firstText time.Duration
	stats     client.CallStats
}

func newWebChatLog(ctx context.Context) *webChatLog {
	return &webChatLog{ctx: ctx, started: time.Now(), status: "rejected", history: "not_requested", delivery: "not_started", errorKind: "none"}
}

func webChatLogFrom(ctx context.Context) *webChatLog {
	v, _ := ctx.Value(webChatLogKey{}).(*webChatLog)
	return v
}

func (l *webChatLog) generation(result *toolLoopResult, err error) {
	if l == nil {
		return
	}
	if result != nil {
		l.stats = result.CallStats
	}
	l.status = "failed"
	l.errorKind = logging.ErrorKind(err)
}

func (l *webChatLog) finish() {
	if l.ctx.Err() != nil && l.delivery != "write_failed" {
		l.delivery = "client_disconnected"
	}
	usage := "unavailable"
	if l.stats.UsageCalls > 0 {
		usage = "reported"
		if l.stats.UsageCalls < l.stats.ModelCalls {
			usage = "partial"
		}
	}
	fields := []any{"status", l.status, "error_kind", l.errorKind, "duration_ms", time.Since(l.started).Milliseconds(),
		"history", l.history, "delivery", l.delivery, "model_calls", l.stats.ModelCalls, "usage", usage}
	if l.firstText > 0 {
		fields = append(fields, "first_text_ms", l.firstText.Milliseconds())
	}
	if l.stats.UsageCalls > 0 {
		fields = append(fields, "input_tokens", l.stats.Usage.PromptTokens,
			"output_tokens", l.stats.Usage.CompletionTokens, "total_tokens", l.stats.Usage.TotalTokens)
	}
	logging.Event(l.ctx, "chat.request.completed", fields...)
}

func completeWebGeneration(ctx context.Context, result *toolLoopResult) {
	if state := webChatLogFrom(ctx); state != nil {
		state.status = "completed"
		if result != nil && containsNoReplyToolCall(result.NewMessages) {
			state.status = "no_reply"
		}
		if state.history == "pending" && (result == nil || len(result.NewMessages) == 0) {
			state.history = "not_needed"
		}
	}
}

func (h *Handler) saveWebReplyHistory(ctx context.Context, session string, messages []storage.ChatMessage, partial bool) error {
	err := h.chatManager.AddMessages(session, messages)
	logWebHistory(ctx, session, len(messages), partial, err)
	return err
}

func logWebHistory(ctx context.Context, session string, count int, partial bool, err error) {
	status := "saved"
	if partial {
		status = "partial_saved"
	}
	if err != nil {
		status = "failed"
	}
	if state := webChatLogFrom(ctx); state != nil {
		state.history = status
		if err != nil {
			state.status = "failed"
			state.errorKind = logging.ErrorKind(err)
		}
	}
	logging.Event(ctx, "history.committed", "session", session, "messages", count, "status", status, "error_kind", logging.ErrorKind(err))
}
