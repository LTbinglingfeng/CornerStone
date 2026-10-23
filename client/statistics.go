package client

import (
	"context"
	"cornerstone/logging"
	"time"
)

// CallRecord contains billing metadata only, never prompts, credentials or errors.
// InputTokens and TotalTokens include cache reads and writes for every provider.
// A call is one AIClient invocation, not an SDK-internal HTTP attempt.
type CallRecord struct {
	CompletedAt         time.Time `json:"completed_at"`
	ProviderID          string    `json:"provider_id"`
	Model               string    `json:"model"`
	Success             bool      `json:"success"`
	UsageReported       bool      `json:"usage_reported"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	TotalTokens         int64     `json:"total_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	ReasoningTokens     int64     `json:"reasoning_tokens"`
	DurationMS          int64     `json:"duration_ms"`
}

type CallRecorder interface {
	RecordCall(CallRecord) error
}

type callRecorderKey struct{}
type callRecording struct {
	recorder CallRecorder
	provider string
}

// WithCallRecorder scopes recording to a generation and all its tool-loop calls.
// The recorder must be concurrency-safe. Recording errors never fail generation.
func WithCallRecorder(ctx context.Context, recorder CallRecorder, providerID string) context.Context {
	return context.WithValue(ctx, callRecorderKey{}, callRecording{recorder, providerID})
}

func recordCompletedCall(ctx context.Context, model string, usage Usage, duration int64, callErr error) {
	recording, ok := ctx.Value(callRecorderKey{}).(callRecording)
	if !ok || recording.recorder == nil {
		return
	}
	record := CallRecord{
		CompletedAt: time.Now().UTC(), ProviderID: recording.provider, Model: model,
		Success: callErr == nil, UsageReported: usage.Present, DurationMS: duration,
	}
	if usage.Present {
		record.InputTokens = int64(usage.PromptTokens)
		record.OutputTokens = int64(usage.CompletionTokens)
		record.TotalTokens = int64(usage.TotalTokens)
		record.CacheReadTokens = int64(usage.CacheReadInputTokens)
		record.CacheCreationTokens = int64(usage.CacheCreationInputTokens)
		record.ReasoningTokens = int64(usage.ReasoningTokens)
	}
	if err := recording.recorder.RecordCall(record); err != nil {
		logging.Event(ctx, "statistics.write.failed", "status", "failed", "error_kind", logging.ErrorKind(err))
	}
}
