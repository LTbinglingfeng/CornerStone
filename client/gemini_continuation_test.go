package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeminiAutomaticContinuationHTTP(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/enabled=%v", stream, enabled), func(t *testing.T) {
				var calls atomic.Int32
				var initialContents any
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hop := int(calls.Add(1))
					wantPath := "/v1beta/models/fixture-model:generateContent"
					if stream {
						wantPath = "/v1beta/models/fixture-model:streamGenerateContent"
					}
					if r.URL.Path != wantPath {
						t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if _, ok := body["automaticContinuation"]; ok {
						t.Error("SDK-only setting leaked upstream")
					}
					if hop == 1 {
						initialContents = body["contents"]
						if body["continuationToken"] != nil {
							t.Error("unexpected initial token")
						}
					} else {
						if !reflect.DeepEqual(initialContents, body["contents"]) {
							t.Error("continuation changed history")
						}
						if body["continuationToken"] != fmt.Sprintf("dG9rZW4%d", hop-1) {
							t.Errorf("continuation token: %v", body["continuationToken"])
						}
					}
					parts := `[ {"text":"reason","thought":true,"thoughtSignature":"c2lnMQ=="},{"text":"A"} ]`
					if hop == 2 {
						parts = `[{"functionCall":{"name":"lookup","args":{"q":"test"}},"thoughtSignature":"c2lnMg=="}]`
					}
					if hop == 3 {
						parts = `[{"text":"B","thoughtSignature":"c2lnMw=="}]`
					}
					finish := "CONTINUATION"
					token := fmt.Sprintf(`,"continuationToken":"dG9rZW4%d"`, hop)
					if hop >= 3 {
						finish = "STOP"
						token = ""
					}
					response := fmt.Sprintf(`{"candidates":[{"index":0,"content":{"role":"model","parts":%s},"finishReason":%q%s}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`, parts, finish, token)
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: %s\n\n", response)
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, response)
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				c := NewGeminiClient(server.URL, "fixture-key")
				req := ChatRequest{Model: "fixture-model", Messages: []Message{{Role: "user", Content: "hello"}}, GeminiAutomaticContinuation: enabled}
				cfg := buildGenAIGenerateContentConfig(req, nil, nil)
				if cfg.AutomaticContinuation == nil || *cfg.AutomaticContinuation != enabled {
					t.Fatal("SDK config lost explicit setting")
				}
				var msg Message
				var finish string
				if stream {
					err := c.ChatStream(ctx, req, func(chunk StreamChunk) error {
						for _, choice := range chunk.Choices {
							msg.Content += choice.Delta.Content
							msg.ReasoningContent += choice.Delta.ReasoningContent
							for _, call := range choice.Delta.ToolCalls {
								msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: call.ID, Type: call.Type, Function: call.Function})
							}
							if choice.Delta.ProviderState != nil {
								msg.ProviderState = choice.Delta.ProviderState
							}
							if choice.FinishReason != "" {
								finish = choice.FinishReason
							}
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					msg.Role = "assistant"
				} else {
					resp, err := c.Chat(ctx, req)
					if err != nil {
						t.Fatal(err)
					}
					if len(resp.Choices) != 1 {
						t.Fatalf("choices: %+v", resp)
					}
					msg = resp.Choices[0].Message
					finish = resp.Choices[0].FinishReason
				}
				wantCalls := int32(1)
				wantText := "A"
				wantFinish := "CONTINUATION"
				if enabled {
					wantCalls = 3
					wantText = "AB"
					wantFinish = "STOP"
				}
				if calls.Load() != wantCalls || msg.Content != wantText || msg.ReasoningContent != "reason" || finish != wantFinish {
					t.Fatalf("calls=%d message=%+v finish=%s", calls.Load(), msg, finish)
				}
				if enabled {
					if len(msg.ToolCalls) != 1 {
						t.Fatalf("tool calls: %+v", msg.ToolCalls)
					}
					// Persist and replay merged state, including signatures from every hop.
					encoded, err := json.Marshal(msg)
					if err != nil {
						t.Fatal(err)
					}
					var restored Message
					if err := json.Unmarshal(encoded, &restored); err != nil {
						t.Fatal(err)
					}
					history := []Message{req.Messages[0], restored, {Role: "tool", ToolCallID: restored.ToolCalls[0].ID, Content: `{"tool":"lookup","ok":true,"data":"result"}`}}
					contents, _, err := c.convertToGenAIContentsForModel(history, req.Model)
					if err != nil {
						t.Fatal(err)
					}
					wire, err := json.Marshal(contents)
					if err != nil {
						t.Fatal(err)
					}
					for _, signature := range []string{"c2lnMQ==", "c2lnMg==", "c2lnMw=="} {
						if !strings.Contains(string(wire), signature) {
							t.Fatalf("signature %s lost: %s", signature, wire)
						}
					}
					if len(contents[1].Parts) != 4 || contents[1].Parts[2].FunctionCall.ID != restored.ToolCalls[0].ID || contents[2].Parts[0].FunctionResponse.Name != "lookup" {
						t.Fatalf("tool replay lost: %s", wire)
					}
				}
			})
		}
	}
}

func TestGeminiAutomaticContinuationCancellation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			var calls atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hop := calls.Add(1)
				if hop > 1 {
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				response := `{"candidates":[{"content":{"role":"model","parts":[{"text":"first"}]},"finishReason":"CONTINUATION","continuationToken":"dG9rZW4="}]}`
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "data: %s\n\n", response)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, response)
				}
			}))
			defer server.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				c := NewGeminiClient(server.URL, "fixture-key")
				req := ChatRequest{Model: "fixture-model", Messages: []Message{{Role: "user", Content: "hello"}}, GeminiAutomaticContinuation: true}
				if stream {
					result <- c.ChatStream(ctx, req, func(StreamChunk) error { return nil })
				} else {
					_, err := c.Chat(ctx, req)
					result <- err
				}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				cancel()
				t.Fatal("no continuation request")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want canceled", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not stop request")
			}
			if calls.Load() != 2 {
				t.Fatalf("extra requests after cancellation: %d", calls.Load())
			}
		})
	}
}

func TestGeminiContinuationStreamCallbackStops(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first\"}]},\"finishReason\":\"CONTINUATION\",\"continuationToken\":\"dG9rZW4=\"}]}\n\n")
	}))
	defer server.Close()
	stopped := errors.New("callback stopped")
	err := NewGeminiClient(server.URL, "fixture-key").ChatStream(context.Background(), ChatRequest{Model: "fixture-model", Messages: []Message{{Role: "user", Content: "hello"}}, GeminiAutomaticContinuation: true}, func(StreamChunk) error { return stopped })
	if !errors.Is(err, stopped) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
