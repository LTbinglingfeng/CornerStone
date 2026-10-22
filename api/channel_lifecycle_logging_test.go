package api

import (
	"context"
	"cornerstone/logging"
	"cornerstone/storage"
	"errors"
	"sync"
	"testing"
)

func TestChannelRequestRetainsCorrelation(t *testing.T) {
	parent := logging.EnsureRequest(context.Background(), "web", "reminder")
	ctx, finish := startChannelRequest(parent, "clawbot", "user_message", "job_id", "job-test")
	defer finish()
	if logging.RequestID(ctx) != logging.RequestID(parent) {
		t.Fatal("nested work replaced request correlation")
	}
	if parent.Value(channelRequestOutcomeKey{}) != nil {
		t.Fatal("mutated parent outcome")
	}
	fresh, done := startChannelRequest(nil, "web", "idle_greeting")
	defer done()
	if logging.RequestID(fresh) == "" {
		t.Fatal("background task has no request")
	}
}

func TestChannelOutcomeDoesNotConfirmUncertainDelivery(t *testing.T) {
	for _, status := range []string{"failed", "partial", "uncertain", "unconfirmed"} {
		t.Run(status, func(t *testing.T) {
			ctx, finish := startChannelRequest(context.Background(), "napcat", "user_message")
			defer finish()
			failure := errors.New("secret upstream detail must not be logged")
			channelOutcome(ctx, status, failure)
			channelOutcome(ctx, "confirmed", nil) // a failure notice is not the reply
			channelHistory(ctx, "outbound", nil)  // durable history is not receipt
			channelFailure(ctx, failure)
			outcome := ctx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome)
			if outcome.status != status || outcome.err != failure {
				t.Fatalf("outcome = %s", outcome.status)
			}
		})
	}
}

func TestChannelOutcomeConcurrentUpdates(t *testing.T) {
	ctx, finish := startChannelRequest(nil, "clawbot", "command")
	defer finish()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			channelOutcome(ctx, "confirmed", nil)
			channelFailure(ctx, context.DeadlineExceeded)
		}()
	}
	wg.Wait()
	outcome := ctx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome)
	if outcome.status != "failed" {
		t.Fatal(outcome.status)
	}
}

func TestChannelQueueResultIsNotDelivery(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "handled"}, {errClawBotDeliveryPending, "queued"}, {context.Canceled, "failed"},
	} {
		if got := channelQueueResult(tc.err); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}

func TestChannelQQOfficialRequestCoversGenerationAndPartialDelivery(t *testing.T) {
	s, remote, job := newQQOfficialTest(t)
	remote.failAt = 2
	var generationCtx context.Context
	s.generate = func(ctx context.Context, _ sessionReplyOptions) (*generatedSessionReply, error) {
		generationCtx = ctx
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "first→second"}}}, nil
	}
	if err := s.process(job); err == nil {
		t.Fatal("expected partial delivery failure")
	}
	if generationCtx == nil || logging.RequestID(generationCtx) == "" {
		t.Fatal("generation not correlated")
	}
	outcome := generationCtx.Value(channelRequestOutcomeKey{}).(*channelRequestOutcome)
	if outcome.status != "partial" {
		t.Fatalf("partial delivery reported as %q", outcome.status)
	}
	if len(remote.sent) != 1 {
		t.Fatal("delivery behavior changed")
	}
	record, ok := s.handler.chatManager.GetSession(qqOfficialSessionID(job.cfg, job.msg.UserOpenID))
	if !ok || len(record.Messages) < 2 || record.Messages[1].Content != "first" {
		t.Fatal("confirmed prefix not committed")
	}
}
