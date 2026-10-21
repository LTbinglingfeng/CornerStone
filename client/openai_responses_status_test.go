package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesRejectsUnsuccessfulTerminalStates(t *testing.T) {
	for _, tc := range []struct{ name, status, details, want string }{
		{"failed", "failed", `,"error":{"code":"server_error","message":"upstream failed"}`, "upstream failed"},
		{"failed_without_details", "failed", "", "failed"},
		{"token_limit", "incomplete", `,"incomplete_details":{"reason":"max_output_tokens"}`, "max_output_tokens"},
		{"filtered", "incomplete", `,"incomplete_details":{"reason":"content_filter"}`, "content_filter"},
		{"incomplete_without_details", "incomplete", "", "incomplete"},
		{"cancelled", "cancelled", "", "cancelled"},
		{"in_progress", "in_progress", "", "in_progress"},
	} {
		for _, transport := range []string{"json", "sse", "sse_as_json", "stream"} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				response := fmt.Sprintf(`{"id":"resp_test","status":%q%s,"output":[{"type":"function_call","call_id":"call_test","name":"dangerous_tool","arguments":"{}"}]}`, tc.status, tc.details)
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
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
					// Terminal events in the protocol are failed/incomplete/completed. A malformed
					// completed event must not override an unsuccessful nested status either.
					event := "response.completed"
					if tc.status == "failed" || tc.status == "incomplete" {
						event = "response." + tc.status
					}
					fmt.Fprintf(w, "data: {\"type\":%q,\"response\":%s}\n\n", event, response)
				}))
				defer server.Close()
				c := NewResponsesClient(server.URL, "test")
				req := ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hello"}}}
				var err error
				if transport == "stream" {
					err = c.ChatStream(context.Background(), req, func(chunk StreamChunk) error {
						for _, choice := range chunk.Choices {
							if choice.FinishReason != "" {
								t.Error("unsuccessful response emitted successful finish")
							}
						}
						return nil
					})
				} else {
					var resp *ChatResponse
					resp, err = c.Chat(context.Background(), req)
					if resp != nil {
						t.Error("unsuccessful response returned executable output")
					}
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want containing %q", err, tc.want)
				}
			})
		}
	}
}

func TestResponsesRequiresSSETerminalEvent(t *testing.T) {
	for _, suffix := range []string{"", "data: [DONE]\n\n"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/suffix=%q", stream, suffix), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"truncated\"}\n\n"+suffix)
				}))
				defer server.Close()
				c := NewResponsesClient(server.URL, "test")
				req := ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hello"}}}
				var err error
				if stream {
					err = c.ChatStream(context.Background(), req, func(StreamChunk) error { return nil })
				} else {
					_, err = c.Chat(context.Background(), req)
				}
				if err == nil {
					t.Fatal("unterminated SSE accepted as success")
				}
			})
		}
	}
}

func TestResponsesCompletedAndLegacyJSON(t *testing.T) {
	for _, status := range []string{"", "completed"} {
		t.Run(status, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"status":%q,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`, status)
			}))
			defer s.Close()
			resp, err := NewResponsesClient(s.URL, "test").Chat(context.Background(), ChatRequest{Model: "test", Messages: []Message{{Role: "user", Content: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Choices[0].Message.Content != "ok" {
				t.Fatalf("unexpected response: %+v", resp)
			}
		})
	}
}
