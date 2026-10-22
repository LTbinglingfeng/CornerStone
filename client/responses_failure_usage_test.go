package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservedResponsesFailureUsage(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, transport := range []string{"json", "sse", "sse_as_json", "completed_with_failed_status", "stream"} {
			for _, usage := range []struct {
				name, json           string
				present              bool
				input, output, total int
			}{
				{"reported", `,"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":5}}`, true, 10, 20, 30},
				{"zero", `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, true, 0, 0, 0},
				{"absent", "", false, 0, 0, 0},
				{"null", `,"usage":null`, false, 0, 0, 0},
			} {
				t.Run(status+"/"+transport+"/"+usage.name, func(t *testing.T) {
					response := fmt.Sprintf(`{"id":"resp_test","model":"actual-model","status":%q%s,"output":[{"type":"function_call","call_id":"call_test","name":"dangerous_tool","arguments":"{}"}]}`, status, usage.json)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if transport == "json" {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, response)
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						if transport == "sse_as_json" {
							w.Header().Set("Content-Type", "application/json")
						}
						event := "response." + status
						if transport == "completed_with_failed_status" {
							event = "response.completed"
						}
						fmt.Fprintf(w, "data: {\"type\":%q,\"response\":%s}\n\n", event, response)
					}))
					defer server.Close()
					c := Observe(NewResponsesClient(server.URL, "test"))
					req := ChatRequest{Model: "requested-model", Messages: []Message{{Role: "user", Content: "hello"}}}
					var err error
					if transport == "stream" {
						err = c.ChatStream(context.Background(), req, func(chunk StreamChunk) error {
							if len(chunk.Choices) != 0 {
								t.Fatal("failure emitted executable output")
							}
							return nil
						})
					} else {
						var resp *ChatResponse
						resp, err = c.Chat(context.Background(), req)
						if resp != nil {
							t.Fatal("failure returned executable output")
						}
						if usage.present {
							var failure *modelUsageError
							if !errors.As(err, &failure) || failure.model != "actual-model" {
								t.Fatalf("missing failure metadata: %v", err)
							}
						}
					}
					if err == nil || !strings.Contains(err.Error(), status) {
						t.Fatalf("expected %s error, got %v", status, err)
					}
					stats := c.Snapshot()
					wantCalls := 0
					if usage.present {
						wantCalls = 1
					}
					if stats.ModelCalls != 1 || stats.UsageCalls != wantCalls || stats.Usage.Present != usage.present || stats.Usage.PromptTokens != usage.input || stats.Usage.CompletionTokens != usage.output || stats.Usage.TotalTokens != usage.total {
						t.Fatalf("unexpected stats: %+v", stats)
					}
					if usage.name == "reported" && (stats.Usage.CacheReadInputTokens != 4 || stats.Usage.ReasoningTokens != 5) {
						t.Fatalf("lost detailed usage: %+v", stats.Usage)
					}
				})
			}
		}
	}
}

func TestModelUsageErrorPreservesCause(t *testing.T) {
	cause := context.DeadlineExceeded
	err := withModelUsage(cause, "test", Usage{Present: true})
	if !errors.Is(err, cause) || err.Error() != cause.Error() {
		t.Fatalf("changed error semantics: %v", err)
	}
	if got := withModelUsage(cause, "test", Usage{}); got != cause {
		t.Fatal("wrapped error without usage")
	}
	if got := withModelUsage(nil, "test", Usage{Present: true}); got != nil {
		t.Fatal("wrapped nil error")
	}
}
