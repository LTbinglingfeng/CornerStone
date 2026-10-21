package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const reasoningOutputFixture = `[
 {"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Think one."}],"content":[{"type":"reasoning_text","text":"not the summary"}],"encrypted_content":"opaque-one","future":{"n":9007199254740993}},
 {"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"lookup","arguments":"{}"},
 {"type":"reasoning","id":"rs_2","summary":[{"type":"summary_text","text":"Think two."}],"encrypted_content":"opaque-two"},
 {"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}
]`

func reasoningFixtureSSE(t *testing.T, completedOnly bool) string {
	t.Helper()
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(reasoningOutputFixture), &items); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if !completedOnly {
		fmt.Fprintln(&b, `data: {"type":"response.output_text.delta","delta":"answer"}`)
		fmt.Fprintln(&b, `data: {"type":"response.reasoning_summary_text.delta","delta":"Think one."}`)
		fmt.Fprintf(&b, "data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":%s}\n", items[1])
		fmt.Fprintln(&b, `data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{}"}`)
		// Deliberately reverse completion arrival; output_index defines replay order.
		for i := len(items) - 1; i >= 0; i-- {
			fmt.Fprintf(&b, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n", i, items[i])
		}
		fmt.Fprintln(&b, `data: {"type":"response.completed","response":{"status":"completed"}}`)
	} else {
		fmt.Fprintf(&b, "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"resolved-model\",\"output\":%s}}\n", strings.Join(strings.Fields(reasoningOutputFixture), " "))
	}
	fmt.Fprintln(&b, "data: [DONE]")
	return b.String()
}

func assertReasoningReplay(t *testing.T, msg Message) {
	t.Helper()
	if msg.ReasoningContent != "Think one.Think two." {
		t.Fatalf("summary = %q", msg.ReasoningContent)
	}
	if msg.ProviderState == nil || msg.ProviderState.Model != "requested-model" {
		t.Fatalf("state = %+v", msg.ProviderState)
	}
	if !strings.Contains(string(msg.ProviderState.Payload), `9007199254740993`) {
		t.Fatal("unknown raw field lost")
	}
	req, err := buildOpenAIResponsesRequest(ChatRequest{Model: "requested-model", Messages: []Message{msg}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(req)
	var wire struct {
		Input   []map[string]json.RawMessage `json:"input"`
		Store   *bool                        `json:"store"`
		Include []string                     `json:"include"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Store == nil || *wire.Store || !reflect.DeepEqual(wire.Include, []string{"reasoning.encrypted_content"}) {
		t.Fatalf("stateless options: %s", raw)
	}
	var types []string
	for _, item := range wire.Input {
		var kind string
		json.Unmarshal(item["type"], &kind)
		types = append(types, kind)
		if kind != "reasoning" {
			for _, field := range []string{"status", "id", "annotations", "logprobs"} {
				if _, ok := item[field]; ok {
					t.Fatalf("output-only field %s", field)
				}
			}
		}
	}
	if !reflect.DeepEqual(types, []string{"reasoning", "function_call", "reasoning", "message"}) {
		t.Fatalf("order = %v", types)
	}
	if string(wire.Input[0]["encrypted_content"]) != `"opaque-one"` || string(wire.Input[2]["id"]) != `"rs_2"` || !strings.Contains(string(wire.Input[0]["summary"]), "Think one.") {
		t.Fatalf("reasoning lost: %s", raw)
	}
}

func TestResponsesReasoningRoundTrip(t *testing.T) {
	for _, mode := range []string{"json", "sse", "sse-completed", "stream", "stream-completed"} {
		t.Run(mode, func(t *testing.T) {
			body := `{"model":"resolved-model","output":` + reasoningOutputFixture + `}`
			if mode != "json" {
				body = reasoningFixtureSSE(t, mode == "sse-completed" || mode == "stream-completed")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode != "json" {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			c := NewResponsesClient(server.URL, "test")
			req := ChatRequest{Model: "requested-model", Messages: []Message{{Role: "user", Content: "question"}}}
			var msg Message
			if strings.HasPrefix(mode, "stream") {
				msg.Role = "assistant"
				var calls []ToolCall
				err := c.ChatStream(context.Background(), req, func(chunk StreamChunk) error {
					for _, choice := range chunk.Choices {
						d := choice.Delta
						msg.Content += d.Content
						msg.ReasoningContent += d.ReasoningContent
						if d.ProviderState != nil {
							msg.ProviderState = d.ProviderState
						}
						for _, tc := range d.ToolCalls {
							if len(calls) == 0 {
								calls = append(calls, ToolCall{ID: tc.ID, Type: tc.Type, Function: ToolCallFunction{Name: tc.Function.Name}})
							}
							calls[0].Function.Arguments += tc.Function.Arguments
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				msg.ToolCalls = calls
			} else {
				resp, err := c.Chat(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				msg = resp.Choices[0].Message
			}
			assertReasoningReplay(t, msg)
		})
	}
}

func TestResponsesReasoningReplaysSuppressedToolCommentary(t *testing.T) {
	var raw openAIResponsesResponse
	if err := json.Unmarshal([]byte(`{"output":`+reasoningOutputFixture+`}`), &raw); err != nil {
		t.Fatal(err)
	}
	msg := convertResponsesToChatResponse(raw, "requested-model").Choices[0].Message
	msg.Content = "" // External-channel delivery keeps only confirmed visible text.
	assertReasoningReplay(t, msg)
}

func TestResponsesReasoningReplayIsolationAndEdits(t *testing.T) {
	var response openAIResponsesResponse
	if err := json.Unmarshal([]byte(`{"output":`+reasoningOutputFixture+`}`), &response); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"provider", "model", "text", "reasoning", "tool", "legacy", "images", "role"} {
		t.Run(change, func(t *testing.T) {
			msg := convertResponsesToChatResponse(response, "requested-model").Choices[0].Message
			model := "requested-model"
			switch change {
			case "provider":
				msg.ProviderState.Provider = "other"
			case "model":
				model = "other"
			case "text":
				msg.Content = "edited"
			case "reasoning":
				msg.ReasoningContent = "edited"
			case "tool":
				msg.ToolCalls[0].Function.Arguments = `{"edited":true}`
			case "legacy":
				msg.ProviderState = nil
			case "images":
				msg.ImagePaths = []string{"not-loaded"}
			case "role":
				msg.Role = "user"
			}
			if _, ok := replayResponsesItems(msg, model); ok {
				t.Fatal("stale or unrelated state replayed")
			}
			if change == "images" {
				return
			}
			req, err := buildOpenAIResponsesRequest(ChatRequest{Model: model, Messages: []Message{msg}})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(req.Input)
			if strings.Contains(string(raw), "encrypted_content") {
				t.Fatalf("state leaked: %s", raw)
			}
			if change == "text" && !strings.Contains(string(raw), "edited") {
				t.Fatal("edit lost")
			}
		})
	}
}
