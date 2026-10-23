package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the public business entry points, not a manually decorated client:
// removing context injection from either entry point must break these tests.
func TestStatisticsBusinessIntegration(t *testing.T) {
	for _, mode := range []string{"web_normal", "web_sse", "web_tool_loop", "channel_reply"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				var req client.ChatRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode upstream: %v", err)
					w.WriteHeader(400)
					return
				}
				if req.Model != "statistics-test" {
					t.Errorf("model = %q", req.Model)
				}
				message := assistantMessage("statistics integration reply")
				if mode == "web_tool_loop" && n == 1 {
					message = assistantMessage("", toolCall("statistics-tool", "send_pat", `{"name":"Test","target":"Fixture"}`))
				}
				if mode == "web_tool_loop" && n == 2 {
					found := false
					for _, msg := range req.Messages {
						if msg.Role == "tool" && msg.ToolCallID == "statistics-tool" {
							found = true
						}
					}
					if !found {
						t.Error("second hop missing tool result")
					}
				}
				resp := chatResp(message)
				resp.Usage = client.Usage{Present: true, PromptTokens: int(n * 10), CompletionTokens: int(n * 2), TotalTokens: int(n * 12)}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					t.Errorf("encode upstream: %v", err)
				}
			}))
			defer server.Close()
			provider := config.DefaultProvider()
			provider.ID, provider.Model, provider.APIKey, provider.BaseURL = "statistics-provider", "statistics-test", "synthetic-key", server.URL+"/v1"
			provider.Type = config.ProviderTypeOpenAI
			h := &Handler{configManager: newTestProviderConfigManager(t, provider), chatManager: storage.NewChatManager(t.TempDir()), userManager: storage.NewUserManager(t.TempDir())}
			manager, err := storage.NewStatisticsManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h.SetStatisticsManager(manager)
			if mode == "channel_reply" {
				session, err := h.chatManager.CreateSession("statistics-session", "fixture", "", "")
				if err != nil {
					t.Fatal(err)
				}
				reply, err := h.generateSessionReply(context.Background(), sessionReplyOptions{Session: session, Channel: chatToolChannelNapCat, EphemeralMessages: []client.Message{{Role: "user", Content: "Synthetic integration message"}}})
				if err != nil {
					t.Fatal(err)
				}
				if reply == nil || reply.Text != "statistics integration reply" {
					t.Fatalf("reply = %+v", reply)
				}
			} else {
				body := fmt.Sprintf(`{"session_id":"statistics-session","messages":[{"role":"user","content":"Synthetic integration message"}],"stream":%t}`, mode == "web_sse")
				w := httptest.NewRecorder()
				h.handleChat(w, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body)))
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "statistics integration reply") {
					t.Fatalf("response: %d %s", w.Code, w.Body.String())
				}
				if mode == "web_sse" && !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
					t.Fatalf("not SSE: %v", w.Header())
				}
			}
			wantCalls, wantInput, wantOutput := int64(1), int64(10), int64(2)
			if mode == "web_tool_loop" {
				wantCalls, wantInput, wantOutput = 2, 30, 6
			}
			if calls.Load() != wantCalls {
				t.Fatalf("upstream calls=%d want=%d", calls.Load(), wantCalls)
			}
			summary, err := manager.Query(storage.StatisticsQuery{}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			totals := summary.Totals
			if totals.Requests != wantCalls || totals.SuccessfulRequests != wantCalls || totals.UsageReportedRequests != wantCalls || totals.FailedRequests != 0 || totals.UnknownUsageRequests != 0 || totals.InputTokens != wantInput || totals.OutputTokens != wantOutput || totals.TotalTokens != wantInput+wantOutput {
				t.Fatalf("unexpected totals (duplicate or missing call): %+v", totals)
			}
			if len(summary.Groups) != 1 || summary.Groups[0].ProviderID != provider.ID || summary.Groups[0].Model != provider.Model {
				t.Fatalf("attribution: %+v", summary.Groups)
			}
		})
	}
}
