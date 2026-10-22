package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

const interactionImageFixture = `{"id":"fixture","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"before"},{"type":"image","mime_type":"image/png","data":"aW1hZ2U="},{"type":"text","text":"after"}]}]}`

type imageRoundTripper func(*http.Request) (*http.Response, error)

func (f imageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestImageInteractionsRequest(t *testing.T) {
	for _, suffix := range []string{"", "/v1beta", "/proxy/v1alpha"} {
		t.Run(suffix, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				path := "/v1beta/interactions"
				if suffix == "/proxy/v1alpha" {
					path = "/proxy/v1alpha/interactions"
				}
				if r.Method != "POST" || r.URL.Path != path || r.URL.RawQuery != "" {
					t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL)
				}
				for key, want := range map[string]string{"X-Goog-Api-Key": "fixture-key", "Content-Type": "application/json", "Accept": "application/json;q=1, text/event-stream;q=0", "Api-Revision": "2026-05-20", "X-Fixture-Transport": "yes"} {
					if got := r.Header.Get(key); got != want {
						t.Errorf("%s=%q, want %q", key, got, want)
					}
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "fixture-model" || body["input"] != "paint" || body["store"] != false || body["stream"] != false {
					t.Errorf("unexpected body: %#v", body)
				}
				format, ok := body["response_format"].(map[string]any)
				if !ok {
					t.Fatalf("missing response_format: %#v", body)
				}
				for key, want := range map[string]string{"type": "image", "delivery": "inline", "aspect_ratio": "16:9", "image_size": "2K", "mime_type": "image/jpeg"} {
					if format[key] != want {
						t.Errorf("response_format[%s]=%v", key, format[key])
					}
				}
				for _, key := range []string{"tools", "previous_interaction_id", "generation_config", "response_modalities", "number_of_images"} {
					if _, ok := body[key]; ok {
						t.Errorf("unexpected %s", key)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, interactionImageFixture)
			}))
			defer server.Close()
			client := NewGeminiImageClient(server.URL+suffix, "fixture-key")
			base := server.Client().Transport
			client.HTTPClient = &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > imageGenerationTimeout || time.Until(deadline) < imageGenerationTimeout-time.Second*10 {
					t.Errorf("missing five minute timeout: %v", deadline)
				}
				r.Header.Set("X-Fixture-Transport", "yes")
				return base.RoundTrip(r)
			})}
			cfg := &ImageGenerationConfig{AspectRatio: "16:9", Size: "2K", MIMEType: "image/jpeg"}
			result, err := client.GenerateImage(context.Background(), "fixture-model", "paint", cfg)
			if err != nil {
				t.Fatal(err)
			}
			if result.MIMEType != "image/png" || string(result.Data) != "image" || result.Text != "before\nafter" {
				t.Fatalf("unexpected result: %#v", result)
			}
			if cfg.Size != "2K" || count.Load() != 1 {
				t.Fatal("config mutation or extra requests")
			}
		})
	}
}

func TestImageInteractionsDefaultAndVersionOverride(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Path != "/v1alpha/interactions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f := body["response_format"].(map[string]any)
		if len(f) != 2 || f["type"] != "image" || f["delivery"] != "inline" {
			t.Errorf("unexpected defaults: %v", f)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, strings.Replace(interactionImageFixture, `{"type":"text","text":"after"}`, `{"type":"image","mime_type":"image/jpeg","data":"c2Vjb25k"}`, 1))
	}))
	defer server.Close()
	c := NewGeminiImageClient(server.URL+"/v1beta", "fixture-key")
	c.APIVersion = "v1alpha"
	result, err := c.GenerateImage(context.Background(), "model", "prompt", nil)
	if err != nil || string(result.Data) != "image" || count.Load() != 1 {
		t.Fatalf("result=%v err=%v requests=%d", result, err, count.Load())
	}
}

func TestImageInteractionsInvalidResponses(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"empty", `{"id":"x","status":"completed"}`, "empty image"},
		{"refused", `{"id":"x","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"cannot comply"}]}]}`, "cannot comply"},
		{"failed", `{"id":"x","status":"failed","errors":[{"code":"SAFETY","message":"blocked"}]}`, "not completed"},
		{"step-error", `{"id":"x","status":"completed","steps":[{"type":"model_output","error":{"code":7,"message":"blocked"}}]}`, "refused or failed"},
		{"incomplete", `{"id":"x","status":"incomplete"}`, "not completed"},
		{"empty-data", strings.Replace(interactionImageFixture, "aW1hZ2U=", "", 1), "empty inline"},
		{"bad-base64", strings.Replace(interactionImageFixture, "aW1hZ2U=", "!!!", 1), "invalid image base64"},
		{"bad-mime", strings.Replace(interactionImageFixture, "image/png", "text/plain", 1), "MIME"},
		{"missing-mime", strings.Replace(interactionImageFixture, `"mime_type":"image/png",`, "", 1), "MIME"},
		{"uri", strings.Replace(interactionImageFixture, `"data":"aW1hZ2U="`, `"uri":"https://example.invalid/image"`, 1), "URI delivery"},
		{"malformed", `{`, "image interaction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := NewGeminiImageClient(server.URL, "fixture-key").GenerateImage(context.Background(), "model", "prompt", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
			if count.Load() != 1 {
				t.Fatalf("requests=%d", count.Load())
			}
		})
	}
}

func TestImageInteractionsNoRetry(t *testing.T) {
	for _, status := range []int{408, 409, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"fixture failure"}}`)
			}))
			defer server.Close()
			_, err := NewGeminiImageClient(server.URL, "fixture-key").GenerateImage(context.Background(), "model", "prompt", nil)
			if err == nil || count.Load() != 1 {
				t.Fatalf("error=%v requests=%d", err, count.Load())
			}
		})
	}
	c := NewGeminiImageClient("https://example.invalid", "fixture-key")
	var count atomic.Int32
	c.HTTPClient = &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) { count.Add(1); return nil, io.ErrUnexpectedEOF })}
	if _, err := c.GenerateImage(context.Background(), "model", "prompt", nil); err == nil || count.Load() != 1 {
		t.Fatalf("connection error=%v requests=%d", err, count.Load())
	}
}

func TestImageInteractionsCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			c := NewGeminiImageClient("https://example.invalid", "fixture-key")
			var count atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				want = context.DeadlineExceeded
			}
			c.HTTPClient = &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
				count.Add(1)
				if !deadline {
					cancel()
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			if _, err := c.GenerateImage(ctx, "model", "prompt", nil); !errors.Is(err, want) {
				t.Fatalf("expected %v: %v", want, err)
			}
			if count.Load() != 1 {
				t.Fatal("retried canceled request")
			}
		})
	}
}

func TestImageInteractionsLocalValidationAndLegacy(t *testing.T) {
	// Host credentials must not be picked up accidentally, including Vertex/ADC.
	t.Setenv("GOOGLE_API_KEY", "env-fixture")
	t.Setenv("GEMINI_API_KEY", "env-fixture")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	c := NewGeminiImageClient("https://example.invalid", "fixture-key")
	var count atomic.Int32
	c.HTTPClient = &http.Client{Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		return nil, fmt.Errorf("unexpected network")
	})}
	for _, cfg := range []*ImageGenerationConfig{{AspectRatio: "bad"}, {Size: "8K"}, {MIMEType: "image/png"}, {MIMEType: "text/plain"}} {
		if _, err := c.GenerateImage(context.Background(), "model", "prompt", cfg); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	for _, v := range []struct{ model, prompt string }{{"", "prompt"}, {"model", " "}} {
		if _, err := c.GenerateImage(context.Background(), v.model, v.prompt, nil); err == nil {
			t.Fatal("accepted missing input")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GenerateImage(ctx, "model", "prompt", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, n := range []int32{0, 1, 4} {
		if _, err := c.GenerateImages(context.Background(), "model", "prompt", &genai.GenerateImagesConfig{NumberOfImages: n}); err == nil || !strings.Contains(err.Error(), "deprecated") {
			t.Fatal(err)
		}
	}
	if _, err := c.GenerateContentImage(context.Background(), "model", "prompt", &genai.GenerateContentConfig{CandidateCount: 2}); err == nil {
		t.Fatal("accepted legacy multiple candidates")
	}
	c.APIKey = ""
	if _, err := c.GenerateImage(context.Background(), "model", "prompt", nil); err == nil || !strings.Contains(err.Error(), "key is required") {
		t.Fatal(err)
	}
	if count.Load() != 0 {
		t.Fatalf("validation made %d requests", count.Load())
	}
}

func TestImageInteractionsLegacyWrapperMapsWithoutMutation(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Path != "/v1beta/interactions" {
			t.Errorf("legacy wrapper used %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		f := body["response_format"].(map[string]any)
		if f["aspect_ratio"] != "4:3" || f["image_size"] != "1K" || f["mime_type"] != "image/jpeg" {
			t.Errorf("lost legacy config: %v", f)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","status":"completed","output_text":"summary","output_image":{"type":"image","mime_type":"image/jpeg","data":"aW1hZ2U="}}`)
	}))
	defer server.Close()
	cfg := &genai.GenerateContentConfig{AutomaticContinuation: genai.Ptr(true), ResponseModalities: []string{"TEXT", "IMAGE"}, ImageConfig: &genai.ImageConfig{AspectRatio: "4:3", ImageSize: "1K", OutputMIMEType: "image/jpeg"}}
	before, _ := json.Marshal(cfg)
	result, err := NewGeminiImageClient(server.URL, "fixture-key").GenerateContentImage(context.Background(), "model", "prompt", cfg)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(cfg)
	if string(before) != string(after) {
		t.Fatal("mutated legacy config")
	}
	if result.Text != "summary" || string(result.Data) != "image" || count.Load() != 1 {
		t.Fatalf("result=%v requests=%d", result, count.Load())
	}
}
