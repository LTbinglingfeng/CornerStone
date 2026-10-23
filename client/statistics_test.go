package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type testCallRecorder struct {
	mu      sync.Mutex
	records []CallRecord
	err     error
}

func (r *testCallRecorder) RecordCall(record CallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return r.err
}

func TestStatisticsObservation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "stream"}[stream], func(t *testing.T) {
			recorder := &testCallRecorder{}
			ctx := WithCallRecorder(context.Background(), recorder, "provider-1")
			u := Usage{Present: true, InputTokensExcludeCache: true, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CacheReadInputTokens: 20, CacheCreationInputTokens: 30}
			fake := observationFake{
				response: &ChatResponse{Model: "actual-model", Usage: u},
				chunks: []StreamChunk{
					{Model: "actual-model", Usage: &Usage{Present: true, PromptTokens: 1}},
					{Usage: &u}, {Usage: &Usage{}},
				},
			}
			observed := Observe(fake)
			start := time.Now()
			if stream {
				if err := observed.ChatStream(ctx, ChatRequest{Model: "alias"}, func(StreamChunk) error { return nil }); err != nil {
					t.Fatal(err)
				}
			} else {
				resp, err := observed.Chat(ctx, ChatRequest{Model: "alias"})
				if err != nil || resp.Usage.PromptTokens != 10 {
					t.Fatalf("response changed: %+v, %v", resp, err)
				}
			}
			if len(recorder.records) != 1 {
				t.Fatalf("records: %+v", recorder.records)
			}
			r := recorder.records[0]
			if !r.Success || !r.UsageReported || r.ProviderID != "provider-1" || r.Model != "actual-model" || r.InputTokens != 60 || r.TotalTokens != 65 || r.CacheReadTokens != 20 || r.CacheCreationTokens != 30 || r.OutputTokens != 5 || r.CompletedAt.Before(start) {
				t.Fatalf("record: %+v", r)
			}
		})
	}
}

func TestStatisticsFailuresAndUnknownUsage(t *testing.T) {
	sentinel := errors.New("upstream failed")
	for _, reported := range []bool{false, true} {
		recorder := &testCallRecorder{err: errors.New("disk failure")}
		ctx := WithCallRecorder(context.Background(), recorder, "p")
		u := Usage{Present: reported}
		observed := Observe(observationFake{response: &ChatResponse{Usage: u}, err: sentinel})
		_, err := observed.Chat(ctx, ChatRequest{Model: "alias"})
		if err != sentinel || len(recorder.records) != 1 {
			t.Fatalf("changed error or missing record: %v %+v", err, recorder.records)
		}
		if r := recorder.records[0]; r.Success || r.UsageReported != reported || r.Model != "alias" {
			t.Fatalf("record: %+v", r)
		}
	}
	// A failed Responses call can still carry real billing data without a response.
	recorder := &testCallRecorder{}
	ctx := WithCallRecorder(context.Background(), recorder, "p")
	observed := Observe(observationFake{err: withModelUsage(sentinel, "actual", Usage{Present: true, TotalTokens: 7})})
	_, err := observed.Chat(ctx, ChatRequest{})
	if !errors.Is(err, sentinel) || recorder.records[0].TotalTokens != 7 || recorder.records[0].Success {
		t.Fatalf("failure usage lost: %+v %v", recorder.records, err)
	}
}

func TestStatisticsContextIsolationAndConcurrency(t *testing.T) {
	recorder := &testCallRecorder{}
	ctx := WithCallRecorder(context.Background(), recorder, "p")
	observed := Observe(observationFake{response: &ChatResponse{}})
	observed.Chat(context.Background(), ChatRequest{}) // Opt-in only.
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { observed.Chat(context.WithoutCancel(ctx), ChatRequest{}) })
	}
	wg.Wait()
	if len(recorder.records) != 20 {
		t.Fatalf("count: %d", len(recorder.records))
	}
}
