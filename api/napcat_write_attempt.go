package api

import (
	"context"
	"sync/atomic"
)

// One marker spans every action in a delivery. Mark immediately before calling
// WriteJSON: even a write error cannot prove that the peer received no bytes.
// In contrast, cancellation while waiting for the write lock is safe to retry
// if no previous segment in the delivery has attempted a write.
type napCatWriteAttemptKey struct{}

type napCatWriteAttempt struct {
	started atomic.Bool
}

func trackNapCatWriteAttempt(ctx context.Context) (context.Context, *napCatWriteAttempt) {
	if ctx == nil {
		ctx = context.Background()
	}
	attempt := &napCatWriteAttempt{}
	return context.WithValue(ctx, napCatWriteAttemptKey{}, attempt), attempt
}

func markNapCatWriteAttempt(ctx context.Context) {
	if attempt, ok := ctx.Value(napCatWriteAttemptKey{}).(*napCatWriteAttempt); ok {
		attempt.started.Store(true)
	}
}
