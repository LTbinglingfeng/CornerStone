package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestObservabilityLogs(t *testing.T) {
	if os.Getenv("CORNERSTONE_REQUEST_LOG_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRequestObservabilityLogs$")
		cmd.Env = append(os.Environ(), "CORNERSTONE_REQUEST_LOG_CHILD=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		text := string(output)
		for _, want := range []string{`event="http.request.started"`, `event="http.request.completed"`, `http_status=400`,
			`event="generation.completed"`, `event="tool.completed"`, `request="test-request"`, `provider="p1"`,
			`model_calls=2`, `input_tokens=12`, `output_tokens=4`, `total_tokens=16`, `usage="reported"`} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %s in %s", want, text)
			}
		}
		if strings.Contains(text, "PRIVATE_SENTINEL") {
			t.Fatalf("private payload leaked: %s", text)
		}
		return
	}
	h := &Handler{}
	request := httptest.NewRequest(http.MethodPost, "/management/test?token=PRIVATE_SENTINEL_QUERY", strings.NewReader("PRIVATE_SENTINEL_BODY"))
	request.Header.Set("Authorization", "Bearer PRIVATE_SENTINEL_TOKEN")
	h.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if logging.RequestID(r.Context()) == "" {
			t.Fatal("HTTP request is not correlated")
		}
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"PRIVATE_SENTINEL_RESPONSE"}`)
	})(httptest.NewRecorder(), request)
	first := chatResp(assistantMessage("PRIVATE_SENTINEL_REPLY", toolCall("c1", "send_pat", `{"name":"PRIVATE_SENTINEL_NAME","target":"friend"}`)))
	first.Usage = client.Usage{Present: true, PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6}
	last := chatResp(assistantMessage("PRIVATE_SENTINEL_FINAL"))
	last.Usage = client.Usage{Present: true, PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}
	ctx := logging.WithFields(context.Background(), "request", "test-request", "source", "web", "trigger", "user_message", "provider", "p1")
	result, err := runChatWithToolLoop(ctx, &fakeAIClient{responses: []*client.ChatResponse{first, last}},
		client.ChatRequest{Model: "m1", Messages: []client.Message{{Role: "user", Content: "PRIVATE_SENTINEL_PROMPT"}}}, newChatToolExecutor(), chatToolContext{SessionID: "s1"}, nil)
	if err != nil || result.CallStats.ModelCalls != 2 || result.CallStats.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
}

func TestGenerationStatsMissingUsageAndFailure(t *testing.T) {
	first := chatResp(assistantMessage("", toolCall("c1", "send_pat", `{"name":"Alice","target":"Bob"}`)))
	first.Usage = client.Usage{Present: true, PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6}
	for _, fail := range []bool{false, true} {
		responses := []*client.ChatResponse{first}
		if !fail {
			responses = append(responses, chatResp(assistantMessage("done"))) // no usage
		}
		result, err := runChatWithToolLoop(context.Background(), &fakeAIClient{responses: responses}, client.ChatRequest{}, newChatToolExecutor(), chatToolContext{}, nil)
		if (err != nil) != fail || result == nil || result.CallStats.ModelCalls != 2 || result.CallStats.UsageCalls != 1 || result.CallStats.Usage.TotalTokens != 6 {
			t.Fatalf("fail=%t result=%+v err=%v", fail, result, err)
		}
	}
}

func TestWebChatOutcomeTracksSSEFailureAndNoReply(t *testing.T) {
	for _, mode := range []string{"normal", "sse_success", "sse_failure", "no_reply", "disconnected"} {
		t.Run(mode, func(t *testing.T) {
			h := &Handler{chatManager: storage.NewChatManager(t.TempDir()), configManager: config.NewManager(filepath.Join(t.TempDir(), "config.json"))}
			ctx, cancel := context.WithCancel(logging.EnsureRequest(context.Background(), "web", "user_message"))
			defer cancel()
			state := newWebChatLog(ctx)
			ctx = context.WithValue(ctx, webChatLogKey{}, state)
			if mode == "disconnected" {
				cancel()
			}
			responses := []*client.ChatResponse{chatResp(assistantMessage("done"))}
			if mode == "sse_failure" {
				responses = nil
			}
			if mode == "no_reply" {
				responses = []*client.ChatResponse{chatResp(assistantMessage("", toolCall("silent", "no_reply", `{}`)))}
			}
			ai := &fakeAIClient{responses: responses}
			r := httptest.NewRequest(http.MethodPost, "/api/chat", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			if strings.HasPrefix(mode, "sse_") {
				h.handleStreamChat(w, r, ai, client.ChatRequest{}, "s1", false, nil, "", "", nil)
			} else {
				h.handleNormalChat(w, r, ai, client.ChatRequest{}, "s1", false, nil, "", "", nil)
			}
			state.finish()
			want := "completed"
			if mode == "sse_failure" {
				want = "failed"
			}
			if mode == "no_reply" {
				want = "no_reply"
			}
			if state.status != want {
				t.Fatalf("status=%s want=%s", state.status, want)
			}
			if mode == "disconnected" && state.delivery != "client_disconnected" {
				t.Fatalf("delivery=%s", state.delivery)
			}
			if mode == "sse_failure" && w.Code != 200 {
				t.Fatalf("changed SSE HTTP behavior: %d", w.Code)
			}
		})
	}
}

func TestAPILogWriterPreservesFlushAndWriteFailure(t *testing.T) {
	w := newAPILogResponseWriter(httptest.NewRecorder())
	w.Flush()
	if !w.wroteHeader || w.status != http.StatusOK {
		t.Fatal("flush did not record implicit 200")
	}
	failing := newAPILogResponseWriter(failedLogWriter{httptest.NewRecorder()})
	if _, err := failing.Write([]byte("body")); err == nil || failing.writeError == nil || failing.bytesWritten != 0 {
		t.Fatal("write failure not captured")
	}
}

type failedLogWriter struct{ *httptest.ResponseRecorder }

func (failedLogWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("closed") }
