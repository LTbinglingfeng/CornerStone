package api

import (
	"context"
	"cornerstone/storage"
	"errors"
	"testing"
	"time"
)

func TestNapCatReminderCanceledBeforeFirstWriteRemainsRetryable(t *testing.T) {
	requests := make(chan napCatActionRequest, 1)
	s := napCatReminderTestService(t, func(req napCatActionRequest) *napCatActionResponse {
		requests <- req
		return &napCatActionResponse{Status: "ok"}
	})
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.sendReminderPrivateText(ctx, storage.ReminderTarget{
		Kind: storage.ReminderTargetKindUser, UserID: "10001", BotSelfID: "20002",
	}, "first→second")
	if !isRetryableReminderError(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unattempted timeout must be retryable: %v", err)
	}
	if len(requests) != 0 {
		t.Fatal("wrote while write lock held")
	}
}

func TestNapCatReminderCancellationBeforeLaterWriteDoesNotReplayPrefix(t *testing.T) {
	requests := make(chan napCatActionRequest, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := napCatReminderTestService(t, func(req napCatActionRequest) *napCatActionResponse {
		requests <- req
		// Cancellation may win the echo race or stop the next action. In either
		// case at least one write was attempted, so replay is unsafe.
		cancel()
		return &napCatActionResponse{Status: "ok"}
	})
	err := s.sendReminderPrivateText(ctx, storage.ReminderTarget{
		Kind: storage.ReminderTargetKindUser, UserID: "10001", BotSelfID: "20002",
	}, "first→second")
	if err == nil || isRetryableReminderError(err) {
		t.Fatalf("attempted prefix must not be replayed: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("unexpected requests: %d", len(requests))
	}
}
