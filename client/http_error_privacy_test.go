package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Capture a fresh process's stdout: logging's default writer is bound at init.
func TestHTTPErrorLogPrivacy(t *testing.T) {
	const helperEnv = "CORNERSTONE_HTTP_ERROR_PRIVACY_HELPER"
	const secret = "PRIVATE_UPSTREAM_BODY_SENTINEL_93821"
	const body = `{"error":{"message":"` + secret + `"}}`
	if os.Getenv(helperEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHTTPErrorLogPrivacy$")
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("privacy helper failed: %v\n%s", err, output)
		}
		text := string(output)
		if strings.Contains(text, secret) {
			t.Fatal("upstream error body leaked into logs")
		}
		if n := strings.Count(text, "status=401"); n != 6 {
			t.Fatalf("expected six HTTP error logs, got %d: %s", n, text)
		}
		if n := strings.Count(text, fmt.Sprintf("body_bytes=%d", len(body))); n != 6 {
			t.Fatalf("missing safe body size metadata: %s", text)
		}
		if n := strings.Count(text, `model="test\nmodel"`); n != 6 {
			t.Fatalf("missing quoted model metadata: %s", text)
		}
		return
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	for _, c := range []AIClient{NewClient(server.URL, "test"), NewAnthropicClient(server.URL, "test"), NewResponsesClient(server.URL, "test")} {
		req := ChatRequest{Model: "test\nmodel", Messages: []Message{{Role: "user", Content: "hello"}}}
		_, err := c.Chat(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), secret) {
			t.Fatal("Chat caller error lost upstream body")
		}
		err = c.ChatStream(context.Background(), req, func(StreamChunk) error { return nil })
		if err == nil || !strings.Contains(err.Error(), secret) {
			t.Fatal("ChatStream caller error lost upstream body")
		}
	}
}
