package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type clawBotRawSend struct {
	Msg struct {
		ToUserID     string                   `json:"to_user_id"`
		ContextToken string                   `json:"context_token"`
		RunID        *string                  `json:"run_id"`
		ItemList     []map[string]interface{} `json:"item_list"`
	} `json:"msg"`
}

// clawBotRawSendServer records sendmessage bodies; replies[i] overrides the
// JSON reply to the i-th (1-based) request.
type clawBotRawSendServer struct {
	mu       sync.Mutex
	requests []clawBotRawSend
	replies  map[int]string
}

func (r *clawBotRawSendServer) serve(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/ilink/bot/sendmessage" {
		http.NotFound(w, req)
		return
	}
	body, _ := io.ReadAll(req.Body)
	var parsed clawBotRawSend
	if err := json.Unmarshal(body, &parsed); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	r.mu.Lock()
	r.requests = append(r.requests, parsed)
	reply, ok := r.replies[len(r.requests)]
	r.mu.Unlock()
	if !ok {
		reply = `{"ret":0}`
	}
	fmt.Fprint(w, reply)
}

func (r *clawBotRawSendServer) snapshot() []clawBotRawSend {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]clawBotRawSend(nil), r.requests...)
}

func clawBotItemText(item map[string]interface{}) string {
	text, _ := item["text_item"].(map[string]interface{})
	value, _ := text["text"].(string)
	return value
}

// Error notices only run under a live worker context.
func clawBotTestWorkerContext(t *testing.T, s *ClawBotService) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.workerCtx = ctx
	s.mu.Unlock()
	t.Cleanup(func() {
		cancel()
		s.notices.Wait()
	})
}

func TestFilterClawBotAuthorizedMessages(t *testing.T) {
	messages := []client.ClawBotIncomingMessage{
		{MessageType: 1, FromUserID: "owner", MessageID: "1"},
		{MessageType: 1, FromUserID: "stranger", MessageID: "2"},
		{MessageType: 1, FromUserID: "", MessageID: "3"},
		{MessageType: 2, FromUserID: "bot", MessageID: "4"},
	}
	got := filterClawBotAuthorizedMessages(config.ClawBotConfig{ILinkUserID: " owner "}, messages)
	if len(got) != 2 || got[0].MessageID != "1" || got[1].MessageID != "4" {
		t.Fatalf("owner filter kept %+v", got)
	}
	if len(messages) != 4 || messages[1].FromUserID != "stranger" {
		t.Fatal("filter mutated its input")
	}
	// Legacy / manual-token accounts have no recorded owner and accept everyone.
	if got := filterClawBotAuthorizedMessages(config.ClawBotConfig{}, messages); len(got) != 4 {
		t.Fatalf("legacy account dropped messages: %+v", got)
	}
}

func TestClawBotDeliverySendsRunIDAndRejectNotice(t *testing.T) {
	recorder := &clawBotRawSendServer{replies: map[int]string{2: `{"ret":-2,"errmsg":"rejected"}`}}
	server := httptest.NewServer(http.HandlerFunc(recorder.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	clawBotTestWorkerContext(t, s)
	j := deliveryTestReply(t, s, cfg)

	deliveryTestStep(t, s, cfg, j.ID) // first chunk sent
	deliveryTestStep(t, s, cfg, j.ID) // history commit
	j = deliveryTestStep(t, s, cfg, j.ID)
	if !j.CancelRequested || j.NextChunk != 1 {
		t.Fatalf("rejected send did not cancel the rest: %+v", j)
	}
	s.notices.Wait()

	requests := recorder.snapshot()
	if len(requests) != 3 {
		t.Fatalf("sends=%d, want chunk, rejected chunk, notice", len(requests))
	}
	for i, req := range requests {
		if req.Msg.RunID == nil || *req.Msg.RunID != j.ID {
			t.Fatalf("request %d run_id=%v, want job id %s", i, req.Msg.RunID, j.ID)
		}
	}
	if got := clawBotItemText(requests[2].Msg.ItemList[0]); got != clawBotSendFailedNotice {
		t.Fatalf("notice text=%q", got)
	}
	if requests[2].Msg.ToUserID != "user" {
		t.Fatalf("notice recipient=%q", requests[2].Msg.ToUserID)
	}
}

func TestClawBotUncertainSendSendsNoNotice(t *testing.T) {
	recorder := &clawBotRawSendServer{replies: map[int]string{1: `not json`}}
	server := httptest.NewServer(http.HandlerFunc(recorder.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	clawBotTestWorkerContext(t, s)
	j := deliveryTestReply(t, s, cfg)
	if j = deliveryTestStep(t, s, cfg, j.ID); j.CancelRequested || j.NextChunk != 1 {
		t.Fatalf("uncertain send not treated as delivered: %+v", j)
	}
	s.notices.Wait()
	if n := len(recorder.snapshot()); n != 1 {
		t.Fatalf("uncertain send produced a notice: sends=%d", n)
	}
}

func TestClawBotUploadFailureSendsNotice(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	clawBotTestWorkerContext(t, s)
	a := fixtureAttachment(t, s)
	f.failUpload = true
	j := stageMediaReply(t, s, cfg, "first", []storage.ChatMessage{{Role: "assistant", Content: "first", Attachments: []storage.ChatAttachment{a}}})
	j = mediaDrive(t, s, cfg, j)
	s.notices.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if j.Status != "cancelled" || len(f.items) != 2 {
		t.Fatalf("job=%s items=%d", j.Status, len(f.items))
	}
	if notice := f.items[1]; notice.Type != 1 || notice.TextItem == nil || notice.TextItem.Text != clawBotUploadFailedNotice {
		t.Fatalf("notice=%+v", notice)
	}
}

func TestClawBotToolProgressSendsStartAndResult(t *testing.T) {
	recorder := &clawBotRawSendServer{}
	server := httptest.NewServer(http.HandlerFunc(recorder.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)

	p := s.startClawBotToolProgress(context.Background(), cfg, "user", "run-1")
	callbacks := p.callbacks()
	callbacks.OnToolCallStart("call-1", "get_time")
	callbacks.OnToolCallStart("call-2", "no_reply")
	callbacks.OnToolCallResult("call-2", "no_reply", "completed")
	callbacks.OnToolCallResult("call-1", "get_time", "weird")
	p.finish()
	callbacks.OnToolCallStart("late", "get_time") // after finish: ignored, no panic

	requests := recorder.snapshot()
	if len(requests) != 2 {
		t.Fatalf("sends=%d, want start+result (no_reply hidden)", len(requests))
	}
	start, result := requests[0].Msg.ItemList[0], requests[1].Msg.ItemList[0]
	if start["type"] != float64(11) || start["is_completed"] != false {
		t.Fatalf("start=%v", start)
	}
	if item, _ := start["tool_call_start_item"].(map[string]interface{}); item["tool_name"] != "get_time" || item["tool_call_id"] != "call-1" {
		t.Fatalf("start item=%v", start)
	}
	if result["type"] != float64(12) || result["is_completed"] != true {
		t.Fatalf("result=%v", result)
	}
	if item, _ := result["tool_call_result_item"].(map[string]interface{}); item["status"] != "unknown" || item["tool_call_id"] != "call-1" {
		t.Fatalf("result item=%v", result)
	}
	for _, req := range requests {
		if req.Msg.RunID == nil || *req.Msg.RunID != "run-1" {
			t.Fatalf("run_id=%v", req.Msg.RunID)
		}
	}

	cfg.ToolProgressDisabled = true
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	disabled := s.startClawBotToolProgress(context.Background(), cfg, "user", "run-2")
	if disabled != nil || disabled.callbacks() != nil {
		t.Fatal("disabled progress still active")
	}
	disabled.finish() // nil-safe
}

func TestRunChatWithToolLoopToolCallCallbacks(t *testing.T) {
	ai := &fakeAIClient{
		t: t,
		responses: []*client.ChatResponse{
			chatResp(assistantMessage("",
				toolCall("call_ok", "send_pat", `{"name":"Alice","target":"Bob"}`),
				toolCall("call_bad", "send_red_packet", `not-json`),
				toolCall("call_blocked", "get_time", `{}`),
			)),
			chatResp(assistantMessage("done")),
		},
	}
	var events []string
	callbacks := &toolLoopCallbacks{
		OnToolCallStart: func(id, name string) { events = append(events, "start:"+id+":"+name) },
		OnToolCallResult: func(id, name, status string) {
			events = append(events, "end:"+id+":"+status)
		},
	}
	_, err := runChatWithToolLoop(context.Background(), ai, client.ChatRequest{Messages: []client.Message{{Role: "user", Content: "hi"}}}, newChatToolExecutor(),
		chatToolContext{AllowedToolNames: map[string]bool{"send_pat": true, "send_red_packet": true}}, callbacks)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"start:call_ok:send_pat", "end:call_ok:completed",
		"start:call_bad:send_red_packet", "end:call_bad:failed",
		"start:call_blocked:get_time", "end:call_blocked:blocked",
	}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events=%v\nwant=%v", events, want)
	}
}
