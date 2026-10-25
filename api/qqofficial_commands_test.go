package api

import (
	"context"
	"cornerstone/storage"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Command jobs after /prompt must use the new generation/configuration: applying
// a persona cancels the old connection and its context.
func qqCommandJob(s *QQOfficialService, base qqOfficialJob, id, text string) qqOfficialJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	base.ctx = context.Background()
	base.generation = s.generation
	base.cfg = s.handler.configManager.GetQQOfficialConfig()
	base.msg.ID, base.msg.Content = id, text
	return base
}

func qqCommandRun(t *testing.T, s *QQOfficialService, base qqOfficialJob, id, text string) qqOfficialJob {
	t.Helper()
	j := qqCommandJob(s, base, id, text)
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func qqCommandCurrent(t *testing.T, s *QQOfficialService, j qqOfficialJob) *storage.ChatRecord {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.currentSessionLocked(j)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func qqCommandNoGeneration(t *testing.T, s *QQOfficialService) {
	t.Helper()
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		t.Error("command reached model generation")
		return nil, fmt.Errorf("unexpected generation")
	}
}

func TestQQOfficialCommandMenuAndListWithoutHistory(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	qqCommandRun(t, s, j, "menu", "/menu")
	qqCommandRun(t, s, j, "list", "/ls")
	if len(f.sent) != 2 || !strings.Contains(f.sent[0], "/new") || !strings.Contains(f.sent[1], "暂无会话") {
		t.Fatalf("replies: %v", f.sent)
	}
	if got := s.handler.chatManager.ListSessions(); len(got) != 0 {
		t.Fatalf("commands created history: %v", got)
	}
}

func TestQQOfficialCommandPermissionsDenyWithoutSideEffects(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandRun(t, s, j, "seed", "hello")
	sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	before, _ := s.handler.chatManager.GetSession(sid)
	c := j.cfg
	c.CommandPermissions = map[string]bool{"new": false, "ls": false, "checkout": false, "re": false, "rename": false, "delete": false, "prompt": false}
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	qqCommandNoGeneration(t, s)
	start := len(f.sent)
	qqCommandRun(t, s, j, "menu", "/menu")
	menu := f.sent[start]
	if !strings.Contains(menu, "/menu") {
		t.Fatalf("menu unavailable: %q", menu)
	}
	for _, name := range []string{"new", "ls", "checkout", "re", "rename", "delete", "prompt"} {
		if strings.Contains(menu, "/"+name) {
			t.Fatalf("disabled command exposed: %s", menu)
		}
	}
	for i, text := range []string{"/new", "/ls", "/checkout 1", "/re", "/rename changed", "/delete current", "/prompt default"} {
		qqCommandRun(t, s, j, fmt.Sprint("denied", i), text)
		if len(f.sent) != start+2+i {
			t.Fatalf("denied command did not reply: %s", text)
		}
	}
	after, ok := s.handler.chatManager.GetSession(sid)
	if !ok || !reflect.DeepEqual(before, after) || len(s.handler.chatManager.ListSessions()) != 1 {
		t.Fatal("denied commands modified history")
	}
	if !reflect.DeepEqual(c, s.handler.configManager.GetQQOfficialConfig()) {
		t.Fatal("denied command modified configuration")
	}
}

func TestQQOfficialCommandSessionLifecycleAndRestart(t *testing.T) {
	s, _, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	qqCommandRun(t, s, j, "first", "/new")
	first := qqCommandCurrent(t, s, j)
	if first == nil || first.SessionID == qqOfficialSessionID(j.cfg, j.msg.UserOpenID) {
		t.Fatal("new command did not allocate a distinct session")
	}
	qqCommandRun(t, s, j, "rename", "/rename First → title")
	first, _ = s.handler.chatManager.GetSession(first.SessionID)
	if first.Title != "First → title" || len(first.Messages) != 0 {
		t.Fatalf("rename: %#v", first)
	}
	qqCommandRun(t, s, j, "second", "/new")
	second := qqCommandCurrent(t, s, j)
	if second.SessionID == first.SessionID {
		t.Fatal("new reused session")
	}
	// Exercise numeric selection using the same order displayed by /ls.
	index := 0
	for i, meta := range s.listSessions(j) {
		if meta.ID == first.SessionID {
			index = i + 1
		}
	}
	qqCommandRun(t, s, j, "checkout", fmt.Sprintf("/checkout %d", index))
	s.mu.Lock()
	s.sessions = nil
	s.mu.Unlock()
	if got := qqCommandCurrent(t, s, j); got == nil || got.SessionID != first.SessionID {
		t.Fatal("selection did not survive store reload")
	}
	qqCommandRun(t, s, j, "delete", "/delete current")
	if _, ok := s.handler.chatManager.GetSession(first.SessionID); ok {
		t.Fatal("delete retained session")
	}
	if got := qqCommandCurrent(t, s, j); got == nil || got.SessionID != second.SessionID {
		t.Fatal("delete did not select remaining session")
	}
	qqCommandRun(t, s, j, "delete-last", "/delete "+second.SessionID)
	if got := qqCommandCurrent(t, s, j); got != nil {
		t.Fatal("last deletion retained active session")
	}
}

func TestQQOfficialCommandLegacySessionCompatibility(t *testing.T) {
	s, _, j := newQQOfficialTest(t)
	sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	if _, err := s.handler.chatManager.CreateSession(sid, "Legacy", "", ""); err != nil {
		t.Fatal(err)
	}
	qqCommandRun(t, s, j, "menu", "/menu")
	if got := qqCommandCurrent(t, s, j); got == nil || got.SessionID != sid {
		t.Fatal("legacy session not selected")
	}
	qqCommandRun(t, s, j, "chat", "continue legacy")
	r, _ := s.handler.chatManager.GetSession(sid)
	if len(r.Messages) != 2 || r.Messages[0].Content != "continue legacy" {
		t.Fatalf("legacy history: %#v", r.Messages)
	}
	qqCommandRun(t, s, j, "new", "/new")
	qqCommandRun(t, s, j, "checkout", "/checkout "+sid)
	qqCommandRun(t, s, j, "rename", "/rename Legacy renamed")
	r, _ = s.handler.chatManager.GetSession(sid)
	if r.Title != "Legacy renamed" || len(r.Messages) != 2 {
		t.Fatal("legacy commands lost history")
	}
}

func TestQQOfficialCommandScopeIsolation(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	qqCommandRun(t, s, j, "own", "/new")
	own := qqCommandCurrent(t, s, j)
	for _, dimension := range []string{"app", "user", "persona", "mismatched-persona"} {
		t.Run(dimension, func(t *testing.T) {
			foreign := j
			switch dimension {
			case "app":
				foreign.cfg.AppID = "other-app"
			case "user":
				foreign.msg.UserOpenID = "other-user"
			case "persona":
				foreign.cfg.PromptID = "other-persona"
			}
			id := qqOfficialSessionID(foreign.cfg, foreign.msg.UserOpenID)
			prompt := foreign.cfg.PromptID
			if dimension == "mismatched-persona" {
				id += "-foreign"
				prompt = "other-persona"
			}
			if _, err := s.handler.chatManager.CreateSession(id, "SECRET-"+dimension, prompt, ""); err != nil {
				t.Fatal(err)
			}
			before, _ := s.handler.chatManager.GetSession(id)
			qqCommandRun(t, s, j, dimension+"-list", "/ls")
			if strings.Contains(f.sent[len(f.sent)-1], "SECRET") {
				t.Fatal("list leaked another scope")
			}
			for _, cmd := range []string{"checkout", "delete"} {
				qqCommandRun(t, s, j, dimension+cmd, "/"+cmd+" "+id)
			}
			after, ok := s.handler.chatManager.GetSession(id)
			if !ok || !reflect.DeepEqual(before, after) {
				t.Fatal("full ID selector modified foreign session")
			}
			if got := qqCommandCurrent(t, s, j); got == nil || got.SessionID != own.SessionID {
				t.Fatal("full ID selector changed active scope")
			}
		})
	}
}

func TestQQOfficialCommandReplayAfterDeletion(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	create := qqCommandRun(t, s, j, "create", "/new")
	rename := qqCommandRun(t, s, j, "rename", "/rename old")
	deletion := qqCommandRun(t, s, j, "delete", "/delete current")
	qqCommandRun(t, s, j, "replacement", "/new")
	replacement := qqCommandCurrent(t, s, j)
	count := len(f.sent)
	s.mu.Lock()
	s.receipts = nil
	s.sessions = nil
	s.mu.Unlock()
	for _, replay := range []qqOfficialJob{create, rename, deletion} {
		if err := s.process(replay); err != nil {
			t.Fatal(err)
		}
	}
	got := qqCommandCurrent(t, s, j)
	if len(f.sent) != count || len(s.handler.chatManager.ListSessions()) != 1 || !reflect.DeepEqual(replacement, got) {
		t.Fatal("replayed command repeated side effect or acknowledgement")
	}
}

func TestQQOfficialCommandParsingAndLiteralEscape(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	for i, text := range []string{"/not-a-command", " ／ＭＥＮＵ　", "/LS", "/NeW"} {
		qqCommandRun(t, s, j, fmt.Sprint(i), text)
	}
	if len(f.sent) != 4 || len(s.handler.chatManager.ListSessions()) != 1 {
		t.Fatalf("parsing replies/sessions: %v / %v", f.sent, s.handler.chatManager.ListSessions())
	}
	if !strings.Contains(f.sent[0], "/menu") || !strings.Contains(f.sent[1], "/menu") {
		t.Fatalf("missing command help: %v", f.sent)
	}
	calls := 0
	s.generate = func(_ context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
		calls++
		if got := o.Session.Messages[len(o.Session.Messages)-1].Content; got != "/new" {
			t.Errorf("escaped literal = %q", got)
		}
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "literal"}}}, nil
	}
	qqCommandRun(t, s, j, "literal", "//new")
	if calls != 1 || len(s.handler.chatManager.ListSessions()) != 1 {
		t.Fatal("escaped command executed instead of chatting")
	}
}

func TestQQOfficialCommandPromptGlobalSwitchAndReplay(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	for _, id := range []string{"alpha", "beta"} {
		if _, err := s.handler.promptManager.Create(id, id, "persona "+id, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	qqCommandRun(t, s, j, "help", "/prompt")
	qqCommandRun(t, s, j, "list", "/prompt ls")
	if !strings.Contains(f.sent[1], "alpha") || !strings.Contains(f.sent[1], "beta") || !strings.Contains(f.sent[1], "default") {
		t.Fatalf("prompt list: %v", f.sent)
	}
	qqCommandRun(t, s, j, "switch-alpha", "/prompt alpha")
	if s.handler.configManager.GetQQOfficialConfig().PromptID != "alpha" {
		t.Fatal("prompt switch not global")
	}
	other := j
	other.msg.UserOpenID = "other-user"
	qqCommandRun(t, s, other, "other-menu", "/menu")
	if !strings.Contains(f.sent[len(f.sent)-1], "alpha") {
		t.Fatal("other user did not see global persona")
	}
	qqCommandRun(t, s, j, "switch-beta", "/prompt 2")
	if s.handler.configManager.GetQQOfficialConfig().PromptID != "beta" {
		t.Fatal("numeric prompt switch failed")
	}
	count := len(f.sent)
	// Replay with fresh connection metadata, not an already invalid old job.
	qqCommandRun(t, s, j, "switch-alpha", "/prompt alpha")
	if len(f.sent) != count || s.handler.configManager.GetQQOfficialConfig().PromptID != "beta" {
		t.Fatal("persona replay was processed again")
	}
	qqCommandRun(t, s, j, "default", "/prompt default")
	if s.handler.configManager.GetQQOfficialConfig().PromptID != "" {
		t.Fatal("default prompt not restored")
	}
	if len(s.handler.chatManager.ListSessions()) != 0 {
		t.Fatal("prompt commands created history")
	}
}

// Hold the acknowledgement in flight while a newer settings update invalidates
// its connection. The old command's deferred apply must not restart that update.
type qqCommandBlockingAck struct {
	*fakeQQOfficial
	entered chan struct{}
	release chan struct{}
}

func (f *qqCommandBlockingAck) SendText(_ context.Context, user, id string, seq int, text string) (string, error) {
	close(f.entered)
	<-f.release
	return f.fakeQQOfficial.SendText(context.Background(), user, id, seq, text)
}

func TestQQOfficialCommandPromptConcurrentConfigInvalidation(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	qqCommandNoGeneration(t, s)
	for _, id := range []string{"alpha", "beta"} {
		if _, err := s.handler.promptManager.Create(id, id, id, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	ack := &qqCommandBlockingAck{fakeQQOfficial: f, entered: make(chan struct{}), release: make(chan struct{})}
	j = qqCommandJob(s, j, "switch", "/prompt alpha")
	j.conn = ack
	done := make(chan error, 1)
	go func() { done <- s.process(j) }()
	<-ack.entered
	c := s.handler.configManager.GetQQOfficialConfig()
	c.PromptID = "beta"
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		close(ack.release)
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	s.mu.Lock()
	generation := s.generation
	s.mu.Unlock()
	close(ack.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	gotGeneration, gotPrompt := s.generation, s.cfg.PromptID
	s.mu.Unlock()
	if gotGeneration != generation || gotPrompt != "beta" || s.handler.configManager.GetQQOfficialConfig().PromptID != "beta" {
		t.Fatal("stale prompt acknowledgement reapplied or overwrote newer configuration")
	}
}
