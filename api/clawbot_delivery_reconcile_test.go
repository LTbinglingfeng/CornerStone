package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClawBotDeliveryCancelReconcilesAcknowledgedPrefix(t *testing.T) {
	transport := &deliveryTestTransport{}
	server := httptest.NewServer(http.HandlerFunc(transport.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	job := deliveryTestReply(t, s, cfg)
	job = deliveryTestStep(t, s, cfg, job.ID) // acknowledged, not yet projected into chat history
	if job.NextChunk != 1 || job.Status != "committing" {
		t.Fatalf("bad ack: %+v", job)
	}
	if err := s.actOnDelivery(cfg, deliveryTestAction(s, cfg, job, "cancel", generateID())); err != nil {
		t.Fatal(err)
	}
	job = deliveryTestJob(t, s, job.ID)
	if job.Status != "cancelled" || !job.HistoryCommitted || job.HistoryChunks != 1 {
		t.Fatalf("lost acknowledged history: %+v", job)
	}
	deliveryTestHistory(t, s, job.SessionID, "first")
	if len(transport.snapshot()) != 1 {
		t.Fatal("cancellation sent another segment")
	}
}

func TestClawBotDeliveryCancelledHistoryConflictKeepsAuditAndCanRetry(t *testing.T) {
	transport := &deliveryTestTransport{}
	server := httptest.NewServer(http.HandlerFunc(transport.serve))
	defer server.Close()
	s, cfg, _ := deliveryTestService(t, server.URL)
	job := deliveryTestReply(t, s, cfg)
	job = deliveryTestStep(t, s, cfg, job.ID)
	if err := s.handler.chatManager.DeleteSession(job.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.actOnDelivery(cfg, deliveryTestAction(s, cfg, job, "cancel", generateID())); err != nil {
		t.Fatal(err)
	}
	job = deliveryTestJob(t, s, job.ID)
	if job.Status != "cancelled" || job.HistoryCommitted || job.LastError == "" {
		t.Fatalf("conflict silently discarded: %+v", job)
	}
	rows := s.deliveryList()
	if len(rows) != 1 || !rows[0].HistoryPending {
		t.Fatal("unreconciled history hidden")
	}
	d := &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{job}}
	for range 101 {
		j := newDeliveryJob("chat", "other", time.Now())
		j.Status = "done"
		j.HistoryCommitted = true
		d.Jobs = append(d.Jobs, j)
	}
	pruneDeliveryJobs(d)
	if deliveryJob(d, job.ID) == nil {
		t.Fatal("pruned uncommitted confirmed audit")
	}
	// Repair the local transcript, then retry history only. No send may be replayed.
	if _, err := s.handler.chatManager.CreateSession(job.SessionID, "repaired", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.actOnDelivery(cfg, deliveryTestAction(s, cfg, job, "retry", generateID())); err != nil {
		t.Fatal(err)
	}
	job = deliveryTestJob(t, s, job.ID)
	if job.Status != "cancelled" || !job.HistoryCommitted {
		t.Fatalf("history-only retry failed: %+v", job)
	}
	deliveryTestHistory(t, s, job.SessionID, "first")
	if len(transport.snapshot()) != 1 {
		t.Fatal("history repair resent acknowledged segment")
	}
}

func TestClawBotDeliveryCancelledUncertainAuditNeverPruned(t *testing.T) {
	job := newDeliveryJob("chat", "user", time.Now())
	job.Status = "cancelled"
	job.CancelRequested = true
	job.HistoryCommitted = true
	job.Chunks = []storage.ClawBotDeliveryChunk{{Status: "uncertain", Text: "potentially delivered"}}
	d := &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{job}}
	for range 101 {
		j := newDeliveryJob("chat", "other", time.Now())
		j.Status = "done"
		j.HistoryCommitted = true
		d.Jobs = append(d.Jobs, j)
	}
	pruneDeliveryJobs(d)
	if deliveryJob(d, job.ID) == nil {
		t.Fatal("pruned unresolved send audit")
	}
}

func TestClawBotDeliveryCommandKeepsNewestContext(t *testing.T) {
	s, cfg, _ := deliveryTestService(t, "http://localhost")
	command := newClawBotTextIncomingMessage("user", "/help")
	command.MessageID = "1"
	command.ContextToken = "old"
	newer := newClawBotTextIncomingMessage("user", "later")
	newer.MessageID = "2"
	newer.ContextToken = "new"
	if err := s.receiveDeliveryBatch(cfg, "", "cursor", []client.ClawBotIncomingMessage{command, newer}); err != nil {
		t.Fatal(err)
	}
	job := s.state.Delivery.Jobs[0]
	s.stepDelivery(context.Background(), cfg, job)
	if s.state.ContextTokens["user"] != "new" {
		t.Fatal("command regressed received context")
	}
	if got := deliveryTestJob(t, s, job.ID); got.Status != "ready" {
		t.Fatalf("command did not stage its response: %+v", got)
	}
}

func TestClawBotDeliveryPersistsMemoryCompletionMarker(t *testing.T) {
	s, cfg, dir := deliveryTestService(t, "http://localhost")
	full := s.handler.configManager.Get()
	full.MemoryEnabled = true
	if err := s.handler.configManager.Update(full); err != nil {
		t.Fatal(err)
	}
	s.handler.memoryManager = storage.NewMemoryManager(t.TempDir())
	record, err := s.handler.chatManager.CreateSession(generateID(), "memory", "persona", "")
	if err != nil {
		t.Fatal(err)
	}
	memory := s.handler.getOrCreateMemorySession("effective-persona", record.SessionID)
	job := newDeliveryJob("chat", "user", time.Now())
	job.Status = "generating"
	deliveryTestInsert(t, s, cfg, job)
	// A silent no_reply turn still completes its memory round; use the actual
	// effective persona, not the possibly stale persona on the chat record.
	if err := s.stageDeliveryReply(cfg, job.ID, record.SessionID, &clawBotGeneratedReply{MemSession: memory}, false, nil); err != nil {
		t.Fatal(err)
	}
	job = deliveryTestStep(t, s, cfg, job.ID)
	if job.Status != "done" || !job.MemoryNotified || job.MemoryPromptID != "effective-persona" {
		t.Fatalf("missing memory marker: %+v", job)
	}
	disk, err := storage.NewClawBotStateManager(dir).Load(clawBotAccount(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if !disk.Delivery.Jobs[0].MemoryNotified {
		t.Fatal("memory dispatch marker not durable")
	}
}
