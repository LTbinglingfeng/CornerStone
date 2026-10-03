package api

import (
	"context"
	"cornerstone/config"
	"cornerstone/storage"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func lifecycleConfig(t *testing.T, h *Handler) {
	t.Helper()
	cfg := h.configManager.Get()
	cfg.NapCat = config.NapCatConfig{Enabled: true, AccessToken: "test-token", AllowPrivate: true, SourceFilterMode: "all"}
	cfg.ReplyWaitWindowSeconds = 60
	if err := h.configManager.Update(cfg); err != nil {
		t.Fatal(err)
	}
}

func lifecycleLogin(t *testing.T, s *NapCatService) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		id := s.selfID
		s.mu.RUnlock()
		if id == 20002 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("login timeout")
}

func TestNapCatLifecycleCanceledWriteAndLockWait(t *testing.T) {
	sent := make(chan struct{}, 4)
	s := newNapCatTestService(t, nil, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "send_private_msg" {
			sent <- struct{}{}
		}
		return napCatActionResponse{Data: []byte(`{"user_id":20002}`)}
	})
	lifecycleLogin(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.sendActionWithEcho(ctx, "send_private_msg", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled action: %v", err)
	}
	s.writeMu.Lock()
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- s.sendAction(ctx, "send_private_msg", nil) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("lock wait: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("write lock wait ignored cancellation")
	}
	s.writeMu.Unlock()
	cancel()
	select {
	case <-sent:
		t.Fatal("canceled request reached websocket")
	default:
	}
}

func TestNapCatLifecycleOldConnectionAndIdentity(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	lifecycleConfig(t, h)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		return napCatActionResponse{Data: []byte(`{"user_id":20002}`)}
	})
	lifecycleLogin(t, s)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	ctx, cancel, err := s.bindSourceContext(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	s.mu.Lock()
	old := s.conn
	replacement := new(websocket.Conn)
	s.conn = replacement
	s.mu.Unlock()
	s.disconnectConnection(old, "stale reader")
	s.mu.RLock()
	current := s.conn
	s.mu.RUnlock()
	if current != replacement {
		t.Fatal("stale reader disconnected replacement")
	}
	if err := s.validateSourceContext(ctx); err == nil {
		t.Fatal("old task accepted by replacement")
	}
	if _, err := s.sendActionWithEcho(ctx, "send_private_msg", nil); err == nil {
		t.Fatal("old task sent")
	}
	s.mu.Lock()
	s.conn = old
	s.mu.Unlock()
}

func TestNapCatLifecyclePermissionRevocation(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	lifecycleConfig(t, h)
	sent := make(chan struct{}, 4)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "send_private_msg" {
			sent <- struct{}{}
		}
		return napCatActionResponse{Data: []byte(`{"user_id":20002}`)}
	})
	lifecycleLogin(t, s)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	ctx, cancel, err := s.bindSourceContext(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, ok := s.enqueuePendingMessage(source, napCatPendingMessage{Text: "queued"}); !ok {
		t.Fatal("enqueue rejected")
	}
	s.mu.RLock()
	queuedCtx := s.pendingReplies[source].Context
	s.mu.RUnlock()
	cfg := h.configManager.GetNapCatConfig()
	cfg.SourceFilterMode = "allowlist"
	cfg.AllowedPrivateUserIDs = []string{"99999"}
	if err := h.configManager.UpdateNapCatConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	s.mu.RLock()
	remaining := len(s.pendingReplies)
	s.mu.RUnlock()
	if remaining != 0 || queuedCtx.Err() == nil {
		t.Fatal("revoked queue not canceled")
	}
	if err := s.validateSourceContext(ctx); err == nil {
		t.Fatal("revoked execution accepted")
	}
	if _, err := s.sendActionWithEcho(ctx, "send_private_msg", nil); err == nil {
		t.Fatal("revoked send accepted")
	}
	if err := s.sendReminderPrivateText(context.Background(), storage.ReminderTarget{Kind: storage.ReminderTargetKindUser, UserID: "10001", BotSelfID: "20002"}, "reminder"); err == nil {
		t.Fatal("reminder bypassed allowlist")
	}
	s.flushPendingReply(nil, source)
	if sessions := h.chatManager.ListSessions(); len(sessions) != 0 {
		t.Fatal("revoked queue persisted history")
	}
	select {
	case <-sent:
		t.Fatal("revoked user received message")
	default:
	}
}

func TestNapCatLifecycleSlidingWindowAndStaleTimer(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	lifecycleConfig(t, h)
	s := NewNapCatService(h)
	defer s.Close()
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	state := &napCatPendingReply{Processing: true, Ready: true, Context: context.Background(), Messages: []napCatPendingMessage{{Text: "second"}}, LastActive: time.Now()}
	s.pendingReplies[source] = state
	s.enqueuePendingMessage(source, napCatPendingMessage{Text: "third"})
	s.mu.RLock()
	version := state.TimerVersion
	ready := state.Ready
	timer := state.Timer
	s.mu.RUnlock()
	if ready {
		t.Fatal("sliding arrival retained expired Ready flag")
	}
	s.flushPendingReplyVersion(state.Context, source, state, version-1)
	s.mu.RLock()
	ready = state.Ready
	unchanged := state.Timer == timer
	s.mu.RUnlock()
	if ready || !unchanged {
		t.Fatal("superseded timer altered current window")
	}
	s.finishPendingReplyProcessing(nil, source)
	s.mu.RLock()
	processing := state.Processing
	count := len(state.Messages)
	s.mu.RUnlock()
	if processing || count != 2 {
		t.Fatal("batch ran before new window expired")
	}
}

func TestNapCatLifecycleActionTimeoutCapsParent(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	s := newNapCatTestService(t, nil, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "test_timeout" {
			started <- struct{}{}
			<-release
		}
		return napCatActionResponse{Data: []byte(`{"user_id":20002}`)}
	})
	lifecycleLogin(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.sendActionWithEcho(ctx, "test_timeout", nil); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not sent")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error: %v", err)
		}
	case <-time.After(napCatActionTimeout + 2*time.Second):
		t.Fatal("action exceeded 10-second timeout")
	}
}

func TestNapCatLifecycleConfigSaveRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	m := config.NewManager(path)
	cfg := config.NapCatConfig{Enabled: true, AllowPrivate: true, AccessToken: "old-token"}
	if err := m.UpdateNapCatConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	cfg.AccessToken = "new-token"
	if err := m.UpdateNapCatConfig(cfg); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if m.GetNapCatConfig().AccessToken != "old-token" {
		t.Fatal("failed save changed live config")
	}
}

func TestNapCatLifecycleDisconnectCancelsPendingTimer(t *testing.T) {
	h := newNapCatQuoteTestHandler(t)
	lifecycleConfig(t, h)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		return napCatActionResponse{Data: []byte(`{"user_id":20002}`)}
	})
	lifecycleLogin(t, s)
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	if _, ok := s.enqueuePendingMessage(source, napCatPendingMessage{Text: "old account"}); !ok {
		t.Fatal("enqueue rejected")
	}
	s.mu.RLock()
	state := s.pendingReplies[source]
	version := state.TimerVersion
	conn := s.conn
	s.mu.RUnlock()
	s.disconnectConnection(conn, "test replacement")
	if state.Context.Err() == nil {
		t.Fatal("disconnect did not cancel pending work")
	}
	s.flushPendingReplyVersion(state.Context, source, state, version)
	s.mu.RLock()
	count := len(s.pendingReplies)
	s.mu.RUnlock()
	if count != 0 {
		t.Fatal("stale timer restored pending work")
	}
	if len(h.chatManager.ListSessions()) != 0 {
		t.Fatal("old account task created a session")
	}
}
