package api

import (
	"context"
	"cornerstone/storage"
	"errors"
	"testing"
	"time"
)

func napCatReminderTestService(t *testing.T, respond func(napCatActionRequest) *napCatActionResponse) *NapCatService {
	t.Helper()
	s := napCatDeliveryTestService(t, respond)
	s.handler.configManager = newNapCatQuoteTestHandler(t).configManager
	lifecycleConfig(t, s.handler)
	s.selfID = 20002
	s.connectedAccessToken = "test-token"
	s.connContext, s.connCancel = context.WithCancel(context.Background())
	t.Cleanup(func() { s.disconnect("") })
	return s
}

func TestNapCatReminderSendFailureDoesNotRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial bool
		failure string
	}{
		{"lost_echo", false, "timeout"},
		{"disconnected", false, "disconnect"},
		{"canceled", false, "cancel"},
		{"partial_lost_echo", true, "timeout"},
		{"partial_disconnected", true, "disconnect"},
		{"partial_rejected", true, "reject"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan napCatActionRequest, 8)
			count := 0
			s := napCatReminderTestService(t, func(req napCatActionRequest) *napCatActionResponse {
				requests <- req
				count++
				if tc.partial && count == 1 {
					return &napCatActionResponse{Status: "ok"}
				}
				if tc.failure == "reject" {
					return &napCatActionResponse{Status: "failed", RetCode: 1}
				}
				return nil // The peer received the request, but its echo is lost.
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- s.sendReminderPrivateText(ctx, storage.ReminderTarget{
					Kind: storage.ReminderTargetKindUser, UserID: "10001", BotSelfID: "20002",
				}, "first→second→third")
			}()
			wantRequests := 1
			if tc.partial {
				wantRequests = 2
			}
			for i := 0; i < wantRequests; i++ {
				select {
				case req := <-requests:
					if req.Action != "send_private_msg" {
						t.Fatalf("unexpected action: %s", req.Action)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("peer did not receive request")
				}
			}
			switch tc.failure {
			case "disconnect":
				s.disconnect("test disconnect after receipt")
			case "cancel":
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || isRetryableReminderError(err) {
					t.Fatalf("send failure must be terminal: %v", err)
				}
				if tc.failure == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected lost-echo timeout: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("send did not terminate")
			}
			if len(requests) != 0 {
				t.Fatal("sent more chunks after failure")
			}
		})
	}
}

func TestNapCatReminderBeforeSendUnavailableRemainsRetryable(t *testing.T) {
	for _, offline := range []bool{true, false} {
		name := "unknown_identity"
		if offline {
			name = "offline"
		}
		t.Run(name, func(t *testing.T) {
			requests := make(chan napCatActionRequest, 1)
			s := napCatReminderTestService(t, func(req napCatActionRequest) *napCatActionResponse {
				requests <- req
				return &napCatActionResponse{Status: "ok"}
			})
			if offline {
				s.disconnect("")
			} else {
				s.selfID = 0
			}
			err := s.sendReminderPrivateText(context.Background(), storage.ReminderTarget{
				Kind: storage.ReminderTargetKindUser, UserID: "10001", BotSelfID: "20002",
			}, "reminder")
			if !isRetryableReminderError(err) {
				t.Fatalf("pre-send unavailability should remain retryable: %v", err)
			}
			if len(requests) != 0 {
				t.Fatal("sent before connection/identity was available")
			}
		})
	}
}
