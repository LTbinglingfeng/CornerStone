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
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func deliveryTestService(t *testing.T, url string) (*ClawBotService, config.ClawBotConfig, string) {
	t.Helper()
	s := newTestClawBotService(t, url)
	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.Enabled = false // Manual actions must not start a poll/model worker.
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s.stateManager = storage.NewClawBotStateManager(dir)
	if err := s.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	return s, cfg, dir
}
func deliveryTestJob(t *testing.T, s *ClawBotService, id string) storage.ClawBotDeliveryJob {
	t.Helper()
	j, ok := s.snapshotDelivery(id)
	if !ok {
		t.Fatalf("missing job %s", id)
	}
	return j
}
func deliveryTestStep(t *testing.T, s *ClawBotService, cfg config.ClawBotConfig, id string) storage.ClawBotDeliveryJob {
	t.Helper()
	s.stepDelivery(context.Background(), cfg, deliveryTestJob(t, s, id))
	return deliveryTestJob(t, s, id)
}
func deliveryTestInsert(t *testing.T, s *ClawBotService, cfg config.ClawBotConfig, jobs ...storage.ClawBotDeliveryJob) {
	t.Helper()
	if err := s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		d.Jobs = append(d.Jobs, jobs...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func deliveryTestAction(s *ClawBotService, cfg config.ClawBotConfig, j storage.ClawBotDeliveryJob, action, operation string) clawBotDeliveryAction {
	req := clawBotDeliveryAction{ID: j.ID, AccountID: clawBotAccount(cfg), Revision: j.Revision, Action: action, OperationID: operation}
	if j.NextChunk < len(j.Chunks) {
		req.ChunkID = j.Chunks[j.NextChunk].ClientID
	}
	return req
}
func deliveryTestHistory(t *testing.T, s *ClawBotService, session string, want ...string) {
	t.Helper()
	record, ok := s.handler.chatManager.GetSession(session)
	if !ok {
		t.Fatal("missing session")
	}
	got := []string{}
	for _, m := range record.Messages {
		got = append(got, m.Content)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %q, want %q", got, want)
	}
}
func deliveryTestReply(t *testing.T, s *ClawBotService, cfg config.ClawBotConfig) storage.ClawBotDeliveryJob {
	t.Helper()
	j := newDeliveryJob("chat", "user", time.Now())
	j.Status = "generating"
	record, err := s.handler.chatManager.CreateSession(generateID(), "delivery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	deliveryTestInsert(t, s, cfg, j)
	if err := s.stageDeliveryReply(cfg, j.ID, record.SessionID, &clawBotGeneratedReply{Text: "first→second→third"}, false, nil); err != nil {
		t.Fatal(err)
	}
	j = deliveryTestJob(t, s, j.ID)
	if len(j.Chunks) != 3 {
		t.Fatalf("chunks = %+v", j.Chunks)
	}
	return j
}

func TestClawBotDeliveryReceiveAtomicAndDeduplicate(t *testing.T) {
	s, cfg, dir := deliveryTestService(t, "http://localhost")
	msg := newClawBotTextIncomingMessage("user", "one")
	msg.MessageID = "101"
	msg.ContextToken = "context"
	if err := s.receiveDeliveryBatch(cfg, "old", "cursor-1", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewClawBotStateManager(dir).Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if disk.GetUpdatesBuf != "cursor-1" || disk.ContextTokens["user"] != "context" || len(disk.Delivery.Jobs) != 1 || len(disk.Delivery.Jobs[0].Inputs) != 1 || disk.Delivery.Receipts[clawBotMessageKey(msg)] != disk.Delivery.Jobs[0].ID {
		t.Fatalf("incomplete receive transaction: %+v", disk)
	}
	if err := s.receiveDeliveryBatch(cfg, "cursor-1", "cursor-2", []client.ClawBotIncomingMessage{msg}); err != nil {
		t.Fatal(err)
	}
	disk, err = s.stateManager.Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if disk.GetUpdatesBuf != "cursor-2" || len(disk.Delivery.Jobs) != 1 || len(disk.Delivery.Jobs[0].Inputs) != 1 || len(disk.Delivery.Receipts) != 1 {
		t.Fatalf("duplicate across cursors: %+v", disk)
	}
	// A refused durable write must advance neither inbox, receipt nor cursor in memory.
	before, err := storage.CloneClawBotState(s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, clawBotAccount(cfg)+".json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	msg.MessageID = "102"
	if err := s.receiveDeliveryBatch(cfg, "cursor-2", "cursor-3", []client.ClawBotIncomingMessage{msg}); err == nil {
		t.Fatal("expected save failure")
	}
	after, err := storage.CloneClawBotState(s.state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed transaction mutated memory")
	}
}

func TestClawBotDeliveryRecovery(t *testing.T) {
	s, cfg, dir := deliveryTestService(t, "http://localhost")
	statuses := []string{"sending", "generating", "executing", "preparing"}
	for _, status := range statuses {
		j := newDeliveryJob("chat", status, time.Now())
		j.Status = status
		j.Chunks = []storage.ClawBotDeliveryChunk{{ClientID: status, Status: "sending"}}
		deliveryTestInsert(t, s, cfg, j)
	}
	restarted := newTestClawBotService(t, cfg.BaseURL)
	restarted.stateManager = storage.NewClawBotStateManager(dir)
	if err := restarted.restoreChannelState(cfg); err != nil {
		t.Fatal(err)
	}
	if err := restarted.recoverDeliveries(cfg); err != nil {
		t.Fatal(err)
	}
	disk, err := restarted.stateManager.Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for i, j := range disk.Delivery.Jobs {
		want := "uncertain"
		if statuses[i] == "preparing" {
			want = "queued"
		}
		if j.Status != want || j.Revision != 2 {
			t.Fatalf("recover %s: %+v", statuses[i], j)
		}
		if i == 0 && j.Chunks[0].Status != "uncertain" {
			t.Fatal("sending chunk not uncertain")
		}
	}
}

func TestClawBotDeliveryQueuedSnapshotMerge(t *testing.T) {
	server, state := newClawBotTestServer(t, "reply")
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	full := s.handler.configManager.Get()
	full.ReplyWaitWindowSeconds = 60
	full.ReplyWaitWindowMode = string(config.ReplyWaitWindowModeSliding)
	if err := s.handler.configManager.Update(full); err != nil {
		t.Fatal(err)
	}
	first := newClawBotTextIncomingMessage("user", "first input")
	first.MessageID = "201"
	if err := s.receiveDeliveryBatch(cfg, "", "a", []client.ClawBotIncomingMessage{first}); err != nil {
		t.Fatal(err)
	}
	old := s.state.Delivery.Jobs[0]
	// Simulate an old scan becoming runnable while the newest receive extends its deadline.
	old.ReadyAt = time.Now().Add(-time.Second)
	if err := s.changeDelivery(cfg, old.ID, func(j *storage.ClawBotDeliveryJob) error { j.ReadyAt = time.Now().Add(time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	second := newClawBotTextIncomingMessage("user", "second input")
	second.MessageID = "202"
	if err := s.receiveDeliveryBatch(cfg, "a", "b", []client.ClawBotIncomingMessage{second}); err != nil {
		t.Fatal(err)
	}
	latest := deliveryTestJob(t, s, old.ID)
	if len(latest.Inputs) != 2 || !latest.ReadyAt.After(time.Now().Add(30*time.Second)) {
		t.Fatalf("merge did not extend deadline: %+v", latest)
	}
	s.stepDelivery(context.Background(), cfg, old)
	latest = deliveryTestJob(t, s, old.ID)
	if latest.Status != "queued" || latest.SessionID != "" {
		t.Fatal("stale scan ignored latest readyAt")
	}
	state.mu.Lock()
	requests := len(state.chatRequests)
	state.mu.Unlock()
	if requests != 0 {
		t.Fatal("generated before latest deadline")
	}
	if err := s.changeDelivery(cfg, old.ID, func(j *storage.ClawBotDeliveryJob) error { j.ReadyAt = time.Now().Add(-time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	s.stepDelivery(context.Background(), cfg, old)
	latest = deliveryTestJob(t, s, old.ID)
	if len(latest.InputMessages) != 2 {
		t.Fatalf("stale scan lost merged input: %+v", latest)
	}
	deliveryTestHistory(t, s, latest.SessionID, "first input", "second input")
}

// Read the complete request before closing the socket: delivery is genuinely ambiguous.
type deliveryTestTransport struct {
	mu         sync.Mutex
	requests   []client.ClawBotSendMessageRequest
	disconnect map[int]bool
}

func (d *deliveryTestTransport) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ilink/bot/sendmessage" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var req client.ClawBotSendMessageRequest
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	d.mu.Lock()
	d.requests = append(d.requests, req)
	drop := d.disconnect[len(d.requests)]
	d.mu.Unlock()
	if drop {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ret":0}`)
}
func (d *deliveryTestTransport) snapshot() []client.ClawBotSendMessageRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]client.ClawBotSendMessageRequest(nil), d.requests...)
}

func TestClawBotDeliveryConfirmOnlyReviewedChunk(t *testing.T) {
	transport := &deliveryTestTransport{disconnect: map[int]bool{2: true, 3: true}}
	server := httptest.NewServer(http.HandlerFunc(transport.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	j := deliveryTestReply(t, s, cfg)
	deliveryTestStep(t, s, cfg, j.ID)
	deliveryTestStep(t, s, cfg, j.ID)
	deliveryTestHistory(t, s, j.SessionID, "first")
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Status != "uncertain" || j.NextChunk != 1 {
		t.Fatalf("second send: %+v", j)
	}
	deliveryTestStep(t, s, cfg, j.ID)
	if len(transport.snapshot()) != 2 {
		t.Fatal("sent past uncertain segment")
	}
	req := deliveryTestAction(s, cfg, j, "confirm_sent", "confirm-second")
	stale := req
	stale.Revision--
	if err := s.actOnDelivery(cfg, stale); err == nil {
		t.Fatal("accepted stale revision")
	}
	wrong := req
	wrong.ChunkID = j.Chunks[2].ClientID
	if err := s.actOnDelivery(cfg, wrong); err == nil {
		t.Fatal("accepted wrong chunk")
	}
	if err := s.actOnDelivery(cfg, req); err != nil {
		t.Fatal(err)
	}
	j = deliveryTestJob(t, s, j.ID)
	if j.NextChunk != 2 || j.Chunks[2].Status != "pending" || !j.Chunks[1].ManualConfirmation {
		t.Fatalf("confirmed more than one chunk: %+v", j)
	}
	deliveryTestHistory(t, s, j.SessionID, "first\nsecond")
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Status != "uncertain" || j.NextChunk != 2 {
		t.Fatalf("third send: %+v", j)
	}
	// Exact replay is idempotent even with a different current uncertain segment.
	if err := s.actOnDelivery(cfg, req); err != nil {
		t.Fatal(err)
	}
	replay := deliveryTestJob(t, s, j.ID)
	if replay.NextChunk != 2 || replay.Status != "uncertain" {
		t.Fatal("operation replay confirmed another chunk")
	}
	reused := deliveryTestAction(s, cfg, replay, "confirm_sent", req.OperationID)
	if err := s.actOnDelivery(cfg, reused); err == nil {
		t.Fatal("reused operation ID accepted different intent")
	}
	retry := deliveryTestAction(s, cfg, replay, "retry", "retry-third")
	if err := s.actOnDelivery(cfg, retry); err != nil {
		t.Fatal(err)
	}
	deliveryTestStep(t, s, cfg, j.ID)
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Status != "done" {
		t.Fatalf("final status %s", j.Status)
	}
	deliveryTestHistory(t, s, j.SessionID, "first→second→third")
	requests := transport.snapshot()
	if len(requests) != 4 {
		t.Fatalf("sends=%d", len(requests))
	}
	for i, want := range []int{0, 1, 2, 2} {
		if requests[i].Msg.ClientID != j.Chunks[want].ClientID {
			t.Fatalf("request %d changed client_id", i)
		}
	}
}

func TestClawBotDeliveryHistoryReceiptAndWriteFailure(t *testing.T) {
	for _, failDisk := range []bool{false, true} {
		t.Run(fmt.Sprintf("diskFailure=%v", failDisk), func(t *testing.T) {
			transport := &deliveryTestTransport{}
			server := httptest.NewServer(http.HandlerFunc(transport.serve))
			defer server.Close()
			s, cfg, _ := deliveryTestService(t, server.URL)
			chatDir := filepath.Join(t.TempDir(), "chats")
			s.handler.chatManager = storage.NewChatManager(chatDir)
			j := deliveryTestReply(t, s, cfg)
			j = deliveryTestStep(t, s, cfg, j.ID)
			if j.Status != "committing" || j.NextChunk != 1 {
				t.Fatalf("send not confirmed: %+v", j)
			}
			if failDisk {
				if err := os.Rename(chatDir, chatDir+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(chatDir, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
				j = deliveryTestStep(t, s, cfg, j.ID)
				if j.Status != "blocked" || j.Phase != "history" {
					t.Fatalf("history failure: %+v", j)
				}
				deliveryTestStep(t, s, cfg, j.ID)
				if len(transport.snapshot()) != 1 {
					t.Fatal("resent confirmed chunk after history failure")
				}
				if err := os.Remove(chatDir); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(chatDir+"-saved", chatDir); err != nil {
					t.Fatal(err)
				}
				if err := s.actOnDelivery(cfg, deliveryTestAction(s, cfg, j, "retry", "retry-history")); err != nil {
					t.Fatal(err)
				}
			} else {
				// Simulate crash between durable history receipt and queue advancement.
				if err := s.handler.chatManager.CommitDeliveryBatch(j.SessionID, fmt.Sprintf("wx-out:%s:%d", j.ID, j.NextChunk), confirmedDeliveryMessages(j), false, nil); err != nil {
					t.Fatal(err)
				}
				s.handler.chatManager = storage.NewChatManager(chatDir)
				deliveryTestStep(t, s, cfg, j.ID)
			}
			deliveryTestHistory(t, s, j.SessionID, "first")
			for i := 0; i < 4; i++ {
				j = deliveryTestStep(t, s, cfg, j.ID)
			}
			if j.Status != "done" || len(transport.snapshot()) != 3 {
				t.Fatalf("status=%s sends=%d", j.Status, len(transport.snapshot()))
			}
			deliveryTestHistory(t, s, j.SessionID, "first→second→third")
		})
	}
}

func TestClawBotDeliveryQueueFIFOBlockedUserIsolation(t *testing.T) {
	transport := &deliveryTestTransport{}
	server := httptest.NewServer(http.HandlerFunc(transport.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	blocked := newDeliveryJob("direct", "blocked-user", time.Now())
	blocked.Status = "blocked"
	makeReady := func(user, text string) storage.ClawBotDeliveryJob {
		j := newDeliveryJob("direct", user, time.Now())
		j.Status = "ready"
		j.Chunks = []storage.ClawBotDeliveryChunk{{ClientID: j.ID, Text: text, Status: "pending"}}
		return j
	}
	behind := makeReady("blocked-user", "must wait")
	other1 := makeReady("other-user", "one")
	other2 := makeReady("other-user", "two")
	deliveryTestInsert(t, s, cfg, blocked, behind, other1, other2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runDeliveryLoop(ctx, cfg) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for deliveryTestJob(t, s, other2.ID).Status != "done" {
		if time.Now().After(deadline) {
			t.Fatal("other user stalled behind blocked user")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	requests := transport.snapshot()
	if len(requests) != 2 || requests[0].Msg.ClientID != other1.ID || requests[1].Msg.ClientID != other2.ID {
		t.Fatalf("FIFO/isolation requests: %+v", requests)
	}
	if deliveryTestJob(t, s, behind.ID).Status != "ready" || deliveryTestJob(t, s, blocked.ID).Status != "blocked" {
		t.Fatal("same-user FIFO bypassed blocked job")
	}
}
