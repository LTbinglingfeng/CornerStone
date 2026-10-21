package api

import (
	"context"
	"cornerstone/client"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolLoopDoesNotExecuteUnsuccessfulResponses(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, streamBody := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sse=%v", status, streamBody), func(t *testing.T) {
				raw := fmt.Sprintf(`{"status":%q,"output":[{"type":"function_call","call_id":"call_pat","name":"send_pat","arguments":"{}"}]}`, status)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if streamBody {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_pat\",\"name\":\"send_pat\",\"arguments\":\"{}\"}}\n\n")
						fmt.Fprintf(w, "data: {\"type\":%q,\"response\":%s}\n\n", "response."+status, raw)
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, raw)
					}
				}))
				defer server.Close()
				executor := newChatToolExecutor()
				calls := 0
				executor.handlers["send_pat"] = func(context.Context, client.ToolCall, chatToolContext) chatToolResult {
					calls++
					return chatToolResult{OK: true}
				}
				result, err := runChatWithToolLoop(context.Background(), client.NewResponsesClient(server.URL, "test"), client.ChatRequest{Model: "test", Messages: []client.Message{{Role: "user", Content: "hello"}}}, executor, chatToolContext{}, nil)
				if err == nil {
					t.Fatal("expected failed/incomplete response error")
				}
				if calls != 0 {
					t.Fatalf("executed %d tools from unsuccessful response", calls)
				}
				if result != nil && len(result.NewMessages) != 0 {
					t.Fatal("unsuccessful output entered history")
				}
			})
		}
	}
}
