package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"errors"
	"sync"
	"time"
)

// channelRequestOutcome is scoped to one synchronous business attempt. It is
// never persisted in the delivery queue and never changes delivery decisions.
type channelRequestOutcome struct {
	mu                sync.Mutex
	status            string
	err               error
	generationContext context.Context
	stats             client.CallStats
}
type channelRequestOutcomeKey struct{}

func startChannelRequest(ctx context.Context, source, trigger string, fields ...any) (context.Context, func()) {
	ctx = logging.EnsureRequest(ctx, source, trigger)
	ctx = logging.WithFields(ctx, fields...)
	outcome := &channelRequestOutcome{status: "skipped"}
	ctx = context.WithValue(ctx, channelRequestOutcomeKey{}, outcome)
	started := time.Now()
	logging.Event(ctx, "channel.request.started")
	return ctx, func() {
		outcome.mu.Lock()
		defer outcome.mu.Unlock()
		fields := []any{"status", outcome.status, "duration_ms", time.Since(started).Milliseconds(), "error_kind", logging.ErrorKind(outcome.err)}
		if outcome.generationContext != nil {
			ctx = outcome.generationContext
			usage := "unavailable"
			if outcome.stats.UsageCalls > 0 {
				usage = "reported"
				if outcome.stats.UsageCalls < outcome.stats.ModelCalls {
					usage = "partial"
				}
				fields = append(fields, "input_tokens", outcome.stats.Usage.PromptTokens, "output_tokens", outcome.stats.Usage.CompletionTokens, "total_tokens", outcome.stats.Usage.TotalTokens)
			}
			fields = append(fields, "model_calls", outcome.stats.ModelCalls, "model_duration_ms", outcome.stats.DurationMS, "usage", usage)
		}
		logging.Event(ctx, "channel.request.completed", fields...)
	}
}

func channelGeneration(ctx context.Context, stats client.CallStats) {
	if outcome, ok := ctx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome); ok {
		outcome.mu.Lock()
		outcome.generationContext = ctx
		outcome.stats = stats
		outcome.mu.Unlock()
	}
}

func channelOutcome(ctx context.Context, status string, err error) {
	if ctx == nil {
		return
	}
	if outcome, ok := ctx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome); ok {
		outcome.mu.Lock()
		// A successful failure notice must not turn the failed business request
		// into a successful reply. Likewise, later history work cannot confirm
		// an uncertain send.
		if err == nil && (outcome.status == "failed" || outcome.status == "partial" || outcome.status == "unconfirmed" || outcome.status == "uncertain") {
			outcome.mu.Unlock()
			return
		}
		outcome.status, outcome.err = status, err
		outcome.mu.Unlock()
	}
}

// History success says nothing about receipt by a remote user.
func channelHistory(ctx context.Context, direction string, err error) {
	status := "committed"
	if err != nil {
		status = "failed"
		channelOutcome(ctx, "failed", err)
	}
	logging.Event(ctx, "channel.history.completed", "direction", direction, "status", status, "error_kind", logging.ErrorKind(err))
}

func channelFailure(ctx context.Context, err error) {
	if ctx == nil || err == nil {
		return
	}
	if outcome, ok := ctx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome); ok {
		outcome.mu.Lock()
		if outcome.status != "partial" && outcome.status != "unconfirmed" && outcome.status != "uncertain" {
			outcome.status = "failed"
		}
		outcome.err = err
		outcome.mu.Unlock()
	}
}

// Pending means durable handoff, not delivery. A nil result can also mean the
// origin was already handled; it is deliberately not called confirmed.
func channelQueueResult(err error) string {
	if errors.Is(err, errClawBotDeliveryPending) {
		return "queued"
	}
	if err != nil {
		return "failed"
	}
	return "handled"
}
