package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestGeminiSDKDoesNotRetryFailedGenerations(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		for _, mode := range []string{"chat", "stream", "content-image"} {
			t.Run(fmt.Sprintf("%s/%d", mode, status), func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprint(w, `{"error":{"code":`+fmt.Sprint(status)+`,"message":"fixture failure","status":"UNAVAILABLE"}}`)
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				chat := NewGeminiClient(server.URL, "fixture-key")
				image := NewGeminiImageClient(server.URL, "fixture-key")
				req := ChatRequest{Model: "fixture-model", Messages: []Message{{Role: "user", Content: "hello"}}}
				var err error
				switch mode {
				case "chat":
					_, err = chat.Chat(ctx, req)
				case "stream":
					err = chat.ChatStream(ctx, req, func(StreamChunk) error { return nil })
				case "content-image":
					_, err = image.GenerateContentImage(ctx, req.Model, "image", nil)
				}
				if err == nil {
					t.Fatal("expected upstream error")
				}
				if count := requests.Load(); count != 1 {
					t.Fatalf("generation was retried: %d requests", count)
				}
			})
		}
	}
}

func TestGeminiSDKDoesNotAutomaticallyContinueGenerations(t *testing.T) {
	for _, mode := range []string{"chat", "stream", "content-image"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := requests.Add(1)
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["continuationToken"] != nil || request["continuation_token"] != nil {
					t.Error("unexpected billable continuation request")
				}
				finish := "CONTINUATION"
				if count > 1 {
					finish = "STOP"
				}
				response := fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":"answer"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":%q,"continuationToken":"dG9rZW4="}]}`, finish)
				if mode == "content-image" {
					response = `{"id":"fixture-image","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"answer"},{"type":"image","mime_type":"image/png","data":"aW1hZ2U="}]}]}`
					if r.URL.Path != "/v1beta/interactions" {
						t.Errorf("unexpected image endpoint: %s", r.URL.Path)
					}
					if request["store"] != false {
						t.Error("image request must disable storage")
					}
				}
				if mode == "stream" {
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
			chat := NewGeminiClient(server.URL, "fixture-key")
			req := ChatRequest{Model: "fixture-model", Messages: []Message{{Role: "user", Content: "hello"}}}
			var err error
			switch mode {
			case "chat":
				var response *ChatResponse
				response, err = chat.Chat(ctx, req)
				if err == nil && response.Choices[0].Message.Content != "answer" {
					t.Fatal("lost generated text")
				}
			case "stream":
				var text string
				err = chat.ChatStream(ctx, req, func(chunk StreamChunk) error {
					for _, choice := range chunk.Choices {
						text += choice.Delta.Content
					}
					return nil
				})
				if err == nil && text != "answer" {
					t.Fatalf("unexpected stream: %q", text)
				}
			case "content-image":
				cfg := &genai.GenerateContentConfig{}
				var result *GeneratedImageResult
				result, err = NewGeminiImageClient(server.URL, "fixture-key").GenerateContentImage(ctx, req.Model, "image", cfg)
				if err == nil && (result.MIMEType != "image/png" || string(result.Data) != "image") {
					t.Fatal("lost generated image")
				}
				if cfg.AutomaticContinuation != nil || len(cfg.ResponseModalities) != 0 {
					t.Fatal("mutated caller's generation config")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if count := requests.Load(); count != 1 {
				t.Fatalf("generation was automatically continued: %d requests", count)
			}
		})
	}
}
