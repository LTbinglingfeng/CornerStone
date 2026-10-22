package logging

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func captureTestLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := logger.Writer()
	var output bytes.Buffer
	logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(previous) })
	return &output
}

func TestEventContextAndSingleLine(t *testing.T) {
	out := captureTestLog(t)
	parent := EnsureRequest(context.Background(), "web", "user_message")
	child := WithFields(parent, "session", "s1", "model", "model\nforged")
	Event(child, "model.completed", "input_tokens", 12, "status", "completed")
	Event(parent, "request.completed")
	text := out.String()
	if strings.Count(text, "\n") != 2 || !strings.Contains(text, `model="model\nforged"`) {
		t.Fatalf("expected escaped single-line events: %s", text)
	}
	if strings.Count(text, `session="s1"`) != 1 {
		t.Fatalf("child fields mutated parent: %s", text)
	}
	if strings.Count(text, `request="`+RequestID(parent)+`"`) != 2 {
		t.Fatal("correlation ID not retained")
	}
	if RequestID(EnsureRequest(child, "internal", "tool")) != RequestID(parent) {
		t.Fatal("nested boundary replaced correlation")
	}
	if RequestID(EnsureRequest(context.Background(), "web", "user_message")) == RequestID(parent) {
		t.Fatal("independent requests share an ID")
	}
}

func TestCallerLocation(t *testing.T) {
	out := captureTestLog(t)
	Infof("caller-check")
	Event(context.Background(), "caller-check")
	if strings.Count(out.String(), "events_test.go:") != 2 {
		t.Fatalf("incorrect caller location: %s", out.String())
	}
}

func TestContextFieldsConcurrent(t *testing.T) {
	out := captureTestLog(t)
	ctx := EnsureRequest(context.Background(), "qqofficial", "user_message")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			Event(WithFields(ctx, "call", i), "model.completed")
		}(i)
	}
	wg.Wait()
	if strings.Count(out.String(), "event=") != 20 {
		t.Fatal("lost concurrent events")
	}
}

func TestErrorKindDoesNotExposeError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "none"}, {fmt.Errorf("private: %w", context.Canceled), "canceled"},
		{context.DeadlineExceeded, "timeout"}, {fmt.Errorf("secret"), "failed"},
	} {
		if got := ErrorKind(tc.err); got != tc.want {
			t.Fatalf("kind=%q want=%q", got, tc.want)
		}
	}
}
