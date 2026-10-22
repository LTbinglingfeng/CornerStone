package api

import (
	"bytes"
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type fakeQQOfficial struct {
	mu        sync.Mutex
	sent      []string
	failAt    int
	onMessage func(context.Context, client.QQOfficialMessage) error
	ready     chan struct{}
	stopped   chan struct{}
}

func (f *fakeQQOfficial) Run(ctx context.Context, on func(context.Context, client.QQOfficialMessage) error, status func(client.QQOfficialStatus)) {
	f.mu.Lock()
	f.onMessage = on
	f.mu.Unlock()
	status(client.QQOfficialStatus{Status: "connected"})
	if f.ready != nil {
		close(f.ready)
	}
	<-ctx.Done()
	if f.stopped != nil {
		close(f.stopped)
	}
}
func (f *fakeQQOfficial) SendText(ctx context.Context, u, id string, seq int, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if seq == f.failAt {
		return "", errors.New("send failed")
	}
	f.sent = append(f.sent, text)
	return "ok", nil
}
func (f *fakeQQOfficial) SendImage(ctx context.Context, u, id string, seq int, data []byte, name string) (string, error) {
	return f.SendText(ctx, u, id, seq, "image:"+name)
}
func newQQOfficialTest(t *testing.T) (*QQOfficialService, *fakeQQOfficial, qqOfficialJob) {
	t.Helper()
	h := &Handler{configManager: newTestProviderConfigManager(t, newTestProvider("test")), chatManager: storage.NewChatManager(t.TempDir()), promptManager: storage.NewPromptManager(t.TempDir()), cachePhotoDir: t.TempDir()}
	c := config.QQOfficialConfig{Enabled: true, AppID: "123", AppSecret: "test-secret", SourceFilterMode: "all"}
	if err := h.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	c = h.configManager.GetQQOfficialConfig()
	f := &fakeQQOfficial{}
	ctx, cancel := context.WithCancel(context.Background())
	s := &QQOfficialService{handler: h, cfg: c, generation: 1, cancel: cancel, queue: make(chan qqOfficialJob, 64), done: make(chan struct{}), factory: func(string, string) qqOfficialConnection { return f }, download: func(context.Context, string) ([]byte, error) { return nil, errors.New("unavailable") }, generate: func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		return &generatedSessionReply{Text: "hello", StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "hello", Timestamp: time.Now()}}}, nil
	}}
	h.qqOfficialService = s
	t.Cleanup(s.Close)
	j := qqOfficialJob{ctx: ctx, generation: 1, cfg: c, conn: f, msg: client.QQOfficialMessage{ID: "event-1", UserOpenID: "opaque-user", Content: "hi"}}
	return s, f, j
}
func TestQQOfficialPrivateFlowDedupeIsolation(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	calls := 0
	s.generate = func(ctx context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
		calls++
		if o.Channel != chatToolChannelQQOfficial || o.ToolOptions.Channel != chatToolChannelQQOfficial {
			t.Fatal("incorrect channel")
		}
		if len(o.Session.Messages) != 1 || o.Session.Messages[0].Role != "user" {
			t.Fatal("input not persisted before generation")
		}
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "hello"}}}, nil
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(f.sent) != 1 {
		t.Fatalf("duplicate generation/send %d %v", calls, f.sent)
	}
	record, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(record.Messages) != 2 || record.Messages[0].Content != "hi" {
		t.Fatalf("history lost: %#v", record.Messages)
	}
	other := j
	other.msg.UserOpenID = "other"
	if err := s.process(other); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("openid isolation")
	}
	c := j.cfg
	c.AppID = "456"
	if qqOfficialSessionID(c, j.msg.UserOpenID) == record.SessionID {
		t.Fatal("appid isolation")
	}
	c = j.cfg
	c.PromptID = "persona"
	if qqOfficialSessionID(c, j.msg.UserOpenID) == record.SessionID {
		t.Fatal("prompt isolation")
	}
	// A fresh service still consults durable receipts rather than regenerating.
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("durable receipt missing")
	}
}
func TestQQOfficialMediaPartialAndNoReply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   int
		silent bool
		want   int
	}{{"complete", 0, false, 3}, {"partial", 2, false, 1}, {"first-failed", 1, false, 0}, {"silent", 0, true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, j := newQQOfficialTest(t)
			f.failAt = tc.fail
			j.msg.Attachments = []client.QQOfficialAttachment{{ContentType: "image/png", URL: "https://invalid.test/x"}, {ContentType: "audio/ogg"}}
			png := []byte("\x89PNG\r\n\x1a\n")
			p, err := s.handler.saveCachePhotoBytes(png)
			if err != nil {
				t.Fatal(err)
			}
			j.msg.Attachments = append(j.msg.Attachments, client.QQOfficialAttachment{ContentType: "image/png", URL: "good"})
			s.download = func(_ context.Context, url string) ([]byte, error) {
				if url == "good" {
					return png, nil
				}
				return nil, errors.New("failed")
			}
			s.generate = func(_ context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
				p = o.Session.Messages[0].ImagePaths[0]
				if !strings.Contains(o.Session.Messages[0].Content, "图片下载失败") || !strings.Contains(o.Session.Messages[0].Content, "不支持的附件") {
					t.Fatal("missing media placeholders")
				}
				return &generatedSessionReply{NoReply: tc.silent, StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "first→second", ImagePaths: []string{p}, ReasoningContent: "private reasoning", ProviderState: &client.ProviderState{}}}}, nil
			}
			err = s.process(j)
			if (err != nil) != (tc.fail > 0) {
				t.Fatalf("error %v", err)
			}
			if len(f.sent) != tc.want {
				t.Fatalf("sent %#v", f.sent)
			}
			record, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
			if tc.fail > 0 && record.Messages[len(record.Messages)-1].Role != "system" {
				t.Fatal("missing failure note")
			}
			{
				last := record.Messages[1]
				if last.ReasoningContent != "private reasoning" {
					t.Fatal("reasoning discarded")
				}
				if tc.fail > 0 && last.ProviderState != nil {
					t.Fatal("partial provider state retained")
				}
				if tc.fail == 2 && (last.Content != "first" || len(last.ImagePaths) > 0) {
					t.Fatalf("partial history %#v", last)
				}
				if tc.silent && (last.Content != "" || len(last.ImagePaths) > 0) {
					t.Fatal("no_reply saved visible content")
				}
			}
			_ = s.process(j)
			if len(f.sent) != tc.want {
				t.Fatal("failed delivery retried")
			}
		})
	}
}
func TestQQOfficialIncomingImageAndPathSafety(t *testing.T) {
	s, _, j := newQQOfficialTest(t)
	s.download = func(context.Context, string) ([]byte, error) { return []byte("\x89PNG\r\n\x1a\n"), nil }
	j.msg.Attachments = []client.QQOfficialAttachment{{ContentType: "image/png"}}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	r, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(r.Messages[0].ImagePaths) != 1 {
		t.Fatal("image absent")
	}
	for _, p := range []string{"/etc/passwd", "cache_photo/../config.json", "cache_photo/a/b", "http://example.test/a"} {
		if _, _, err := s.imageBytes(p); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("secret"), 0600)
	os.Symlink(secret, filepath.Join(s.handler.cachePhotoDir, "link.png"))
	if _, _, err := s.imageBytes("cache_photo/link.png"); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestQQOfficialGenerationCancellation(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	started := make(chan struct{})
	release := make(chan struct{})
	s.generate = func(ctx context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
		close(started)
		<-release
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "late"}}}, nil
	}
	done := make(chan error, 1)
	go func() { done <- s.process(j) }()
	<-started
	c := j.cfg
	c.Enabled = false
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	close(release)
	<-done
	if len(f.sent) > 0 {
		t.Fatal("old generation sent")
	}
	if j.ctx.Err() == nil {
		t.Fatal("old connection not cancelled")
	}
	r, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(r.Messages) != 1 {
		t.Fatal("late history committed")
	}
}
func TestQQOfficialToolIsolation(t *testing.T) {
	names := buildToolNameSet(getChatTools(chatToolOptions{Channel: chatToolChannelQQOfficial}))
	for _, name := range []string{"schedule_reminder", "send_pat", "send_red_packet"} {
		if names[name] {
			t.Fatalf("unsafe tool %s", name)
		}
	}
	if !names["no_reply"] || !names[sendAttachmentToolName] {
		t.Fatal("missing no_reply")
	}
	e := newChatToolExecutor()
	result := e.handleScheduleReminder(context.Background(), client.ToolCall{}, chatToolContext{Channel: chatToolChannelQQOfficial})
	if !strings.Contains(result.Error, "unavailable") {
		t.Fatalf("executor did not deny: %#v", result)
	}
}
func TestQQOfficialSettingsProtectedAndSecret(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	auth := newTestAuthHandler(t)
	h := s.handler
	h.authManager = auth.authManager
	h.tokenStore = auth.tokenStore
	h.userManager = auth.userManager
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	for _, url := range []string{"/api/settings/qq-official", "/api/channel/qq-official/login/start", "/api/channel/qq-official/login/status?id=x", "/api/channel/qq-official/login/cancel"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code == 200 {
			t.Fatalf("unprotected %s", url)
		}
	}
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/settings/qq-official", bytes.NewBufferString(`{"enabled":false,"app_id":"123","source_filter_mode":"all","app_secret":""}`))
		h.handleQQOfficialSettings(w, req)
		if w.Code != 200 || strings.Contains(w.Body.String(), "test-secret") || strings.Contains(w.Body.String(), "app_secret") {
			t.Fatalf("settings response %s", w.Body)
		}
		if h.configManager.GetQQOfficialConfig().AppSecret != "test-secret" {
			t.Fatal("blank secret overwritten")
		}
	}
}
func TestQQOfficialLoginLifecycle(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	s.startBinding = func(context.Context) (*client.QQOfficialBinding, error) {
		return &client.QQOfficialBinding{TaskID: "private-task", Key: []byte("private-key"), URL: "https://q.qq.com/qr", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	polls := 0
	s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
		polls++
		return &client.QQOfficialBindResult{Status: "pending"}, nil
	}
	data, err := s.startLogin()
	if err != nil {
		t.Fatal(err)
	}
	id := data["id"].(string)
	if id == "private-task" || len(id) != 48 {
		t.Fatal("id not opaque")
	}
	expiry, _ := time.Parse(time.RFC3339, data["expires_at"].(string))
	if time.Until(expiry) > 5*time.Minute {
		t.Fatal("unbounded TTL")
	}
	if _, err = s.loginStatus("tampered", false); err == nil {
		t.Fatal("tampered id accepted")
	}
	for i := 0; i < 3; i++ {
		if _, err = s.loginStatus(id, false); err != nil {
			t.Fatal(err)
		}
	}
	if polls != 1 {
		t.Fatalf("poll throttle failed %d", polls)
	}
	result, _ := s.loginStatus(id, true)
	if result["status"] != "cancelled" || s.login.binding != nil {
		t.Fatal("cancel failed")
	}
	data, _ = s.startLogin()
	id = data["id"].(string)
	s.mu.Lock()
	s.login.expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	result, _ = s.loginStatus(id, false)
	if result["status"] != "expired" {
		t.Fatal("expiry failed")
	}
	data, _ = s.startLogin()
	id = data["id"].(string)
	s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
		return &client.QQOfficialBindResult{Status: "created", AppID: "999", AppSecret: "new-secret"}, nil
	}
	result, err = s.loginStatus(id, false)
	if err != nil || result["status"] != "created" {
		t.Fatalf("complete %v %v", result, err)
	}
	c := s.handler.configManager.GetQQOfficialConfig()
	if c.AppID != "999" || c.AppSecret != "new-secret" || c.SourceFilterMode != "all" {
		t.Fatalf("credentials not saved")
	}
	if _, ok := result["app_secret"]; ok {
		t.Fatal("secret leaked")
	}
}
func TestQQOfficialLoginStalePoll(t *testing.T) {
	for _, action := range []string{"cancel", "manual", "new", "close"} {
		t.Run(action, func(t *testing.T) {
			s, _, _ := newQQOfficialTest(t)
			s.startBinding = func(context.Context) (*client.QQOfficialBinding, error) {
				return &client.QQOfficialBinding{ExpiresAt: time.Now().Add(time.Minute)}, nil
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
				close(entered)
				<-release
				return &client.QQOfficialBindResult{Status: "created", AppID: "999", AppSecret: "late"}, nil
			}
			data, _ := s.startLogin()
			id := data["id"].(string)
			done := make(chan struct{})
			go func() { s.loginStatus(id, false); close(done) }()
			<-entered
			switch action {
			case "cancel":
				s.loginStatus(id, true)
			case "manual":
				c := s.handler.configManager.GetQQOfficialConfig()
				c.Enabled = false
				s.handler.configManager.UpdateQQOfficialConfig(c)
				s.ApplyCurrentConfig()
			case "new":
				s.startLogin()
			case "close":
				s.Close()
			}
			close(release)
			<-done
			if s.handler.configManager.GetQQOfficialConfig().AppID != "123" {
				t.Fatal("stale credentials won")
			}
		})
	}
}

func TestQQOfficialQueueFilteringAndClose(t *testing.T) {
	s, f, _ := newQQOfficialTest(t)
	f.ready = make(chan struct{})
	f.stopped = make(chan struct{})
	c := s.cfg
	c.SourceFilterMode = "allowlist"
	c.AllowedUserOpenIDs = []string{"opaque-allowed"}
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	<-f.ready
	f.mu.Lock()
	on := f.onMessage
	f.mu.Unlock()
	if err := on(context.Background(), client.QQOfficialMessage{ID: "denied", UserOpenID: "other"}); err != nil {
		t.Fatal(err)
	}
	if len(s.queue) != 0 {
		t.Fatal("allowlist ignored")
	}
	for i := 0; i < 64; i++ {
		if err := on(context.Background(), client.QQOfficialMessage{ID: "event", UserOpenID: "opaque-allowed"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := on(context.Background(), client.QQOfficialMessage{ID: "overflow", UserOpenID: "opaque-allowed"}); err == nil {
		t.Fatal("queue not bounded")
	}
	s.Close()
	select {
	case <-f.stopped:
	case <-time.After(time.Second):
		t.Fatal("connection not stopped")
	}
	for len(s.queue) > 0 {
		j := <-s.queue
		if err := s.process(j); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.handler.chatManager.ListSessions()) != 0 {
		t.Fatal("cancelled queued input persisted")
	}
}
func TestQQOfficialLoginSaveFailure(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	path := filepath.Join(t.TempDir(), "config.json")
	s.handler.configManager = config.NewManager(path)
	c := s.cfg
	s.handler.configManager.UpdateQQOfficialConfig(c)
	s.startBinding = func(context.Context) (*client.QQOfficialBinding, error) {
		return &client.QQOfficialBinding{ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
		return &client.QQOfficialBindResult{Status: "created", AppID: "456", AppSecret: "not-saved"}, nil
	}
	data, err := s.startLogin()
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	os.Mkdir(path, 0700)
	os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0600)
	data, err = s.loginStatus(data["id"].(string), false)
	if err != nil {
		t.Fatal(err)
	}
	if data["status"] != "error" || s.handler.configManager.GetQQOfficialConfig().AppID != "123" {
		t.Fatal("save failure reported as success")
	}
}
func TestQQOfficialConfirmedSaveFailureDoesNotResend(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	dir := t.TempDir()
	s.handler.chatManager = storage.NewChatManager(dir)
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		// Make the session destination a nonempty directory after inbound commit.
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			p := filepath.Join(dir, entry.Name())
			os.Remove(p)
			os.Mkdir(p, 0700)
			os.WriteFile(filepath.Join(p, "block"), []byte("x"), 0600)
		}
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "confirmed"}}}, nil
	}
	if err := s.process(j); err == nil {
		t.Fatal("expected history save failure")
	}
	if len(f.sent) != 1 {
		t.Fatal("send was not confirmed")
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatal("confirmed send retried")
	}
}

func TestQQOfficialRestartAndPersonaBinding(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	dir := t.TempDir()
	s.handler.chatManager = storage.NewChatManager(dir)
	if _, err := s.handler.promptManager.Create("persona", "Persona", "Be kind", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	// Reload from disk, then change persona: the same upstream event stays deduped.
	s.handler.chatManager = storage.NewChatManager(dir)
	c := j.cfg
	c.PromptID = "persona"
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.cfg = s.handler.configManager.GetQQOfficialConfig()
	s.generation++
	j.generation = s.generation
	j.cfg = s.cfg
	s.mu.Unlock()
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatal("replayed event regenerated after restart/persona rebind")
	}
	j.msg.ID = "fresh-event"
	s.generate = func(_ context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
		if o.PromptID != "persona" || o.Persona != "Be kind" || o.PromptName != "Persona" {
			t.Fatalf("persona not bound: %#v", o)
		}
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "bound"}}}, nil
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	if len(s.handler.chatManager.ListSessions()) != 2 {
		t.Fatal("persona sessions were merged")
	}
}

func TestQQOfficialImageToolConfirmation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, f, j := newQQOfficialTest(t)
			if fail {
				f.failAt = 1
			}
			s.download = func(context.Context, string) ([]byte, error) { return []byte("\x89PNG\r\n\x1a\n"), nil }
			j.msg.Attachments = []client.QQOfficialAttachment{{ContentType: "image/png"}}
			s.generate = func(_ context.Context, o sessionReplyOptions) (*generatedSessionReply, error) {
				p := o.Session.Messages[0].ImagePaths[0]
				e := newChatToolExecutor()
				call := client.ToolCall{}
				call.Function.Arguments = `{"path":"` + p + `"}`
				result := e.handleSendAttachment(context.Background(), call, chatToolContext{Channel: chatToolChannelQQOfficial, Attachments: sessionToolAttachments(o.Session.Messages)})
				if !result.OK {
					t.Fatal(result.Error)
				}
				result.Tool = sendAttachmentToolName
				b, _ := json.Marshal(result)
				return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "tool", ToolCallID: "one", Content: string(b)}, {Role: "tool", ToolCallID: "two", Content: string(b)}, {Role: "assistant", ImagePaths: []string{p}}}}, nil
			}
			err := s.process(j)
			if (err != nil) != fail {
				t.Fatal(err)
			}
			r, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
			for _, m := range r.Messages[1:] {
				if fail {
					if len(m.ImagePaths) > 0 || len(m.Attachments) > 0 {
						t.Fatal("failed metadata persisted")
					}
					if _, ok := attachmentFromToolResult(m.Content); ok {
						t.Fatal("failed intent authorizes media")
					}
				}
			}
			if !fail && len(f.sent) != 1 {
				t.Fatalf("not deduplicated: %v", f.sent)
			}
		})
	}
}

func TestQQOfficialLimitsAndEmptyReply(t *testing.T) {
	s, _, j := newQQOfficialTest(t)
	text := qqOfficialInputText(strings.Repeat("图", 30000))
	if len(text) > 64<<10 || !utf8.ValidString(text) || !strings.Contains(text, "已截断") {
		t.Fatal("invalid truncation")
	}
	os.WriteFile(filepath.Join(s.handler.cachePhotoDir, "large.png"), make([]byte, (5<<20)+1), 0600)
	if _, _, err := s.imageBytes("cache_photo/large.png"); err == nil {
		t.Fatal("oversized image accepted")
	}
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		return &generatedSessionReply{NoReply: true, StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "hidden", ProviderState: &client.ProviderState{}}}}, nil
	}
	if err := s.process(j); err != nil {
		t.Fatal(err)
	}
	r, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(r.Messages) != 1 {
		t.Fatal("empty assistant saved")
	}
	c := j.cfg
	c.Enabled = false
	s.handler.configManager.UpdateQQOfficialConfig(c)
	s.ApplyCurrentConfig()
	if s.settingsLocked()["status"] != "disabled" {
		t.Fatal("disabled status")
	}
	s.handler.promptManager = nil
	if s.settingsLocked()["prompt_name"] != "" {
		t.Fatal("nil prompt manager")
	}
}

func TestQQOfficialImageAuthorization(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	p, _ := s.handler.saveCachePhotoBytes([]byte("image"))
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		b, _ := json.Marshal(chatToolResult{OK: true, Tool: sendAttachmentToolName, Data: map[string]interface{}{"attachment": storage.ChatAttachment{Path: p, Kind: "image"}}})
		return &generatedSessionReply{StorageMessages: []storage.ChatMessage{{Role: "tool", ToolCallID: "foreign", Content: string(b)}, {Role: "assistant", Content: "must not claim it was sent"}}}, nil
	}
	if err := s.process(j); err == nil {
		t.Fatal("unauthorized intent must stop delivery")
	}
	if len(f.sent) != 0 {
		t.Fatal("foreign image sent")
	}
	e := newChatToolExecutor()
	call := client.ToolCall{}
	call.Function.Arguments = `{"path":"tts_audio/a.mp3"}`
	result := e.handleSendAttachment(context.Background(), call, chatToolContext{Channel: chatToolChannelQQOfficial, Attachments: map[string]storage.ChatAttachment{"tts_audio/a.mp3": {Kind: "audio"}}})
	if result.OK {
		t.Fatal("audio accepted")
	}
}

func TestQQOfficialGenerationFailureNotifiesSenderWithoutHistory(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		return nil, errors.New("upstream timeout test-secret")
	}
	err := s.process(j)
	if err == nil || !strings.Contains(err.Error(), "生成回复失败") {
		t.Fatalf("generation failure not reported: %v", err)
	}
	if len(f.sent) != 1 || !strings.Contains(f.sent[0], "暂时无法处理你的消息") || !strings.Contains(f.sent[0], "upstream timeout") {
		t.Fatalf("failure notice not sent: %#v", f.sent)
	}
	if strings.Contains(f.sent[0], "test-secret") {
		t.Fatal("secret leaked to sender")
	}
	record, _ := s.handler.chatManager.GetSession(qqOfficialSessionID(j.cfg, j.msg.UserOpenID))
	if len(record.Messages) != 1 || record.Messages[0].Role != "user" {
		t.Fatalf("failure notice persisted: %#v", record.Messages)
	}
	// The receipt was committed with the input, so a replay neither regenerates nor re-notifies.
	if err := s.process(j); err != nil || len(f.sent) != 1 {
		t.Fatalf("replay re-notified: %v %#v", err, f.sent)
	}
}

func TestQQOfficialGenerationFailureAfterInvalidationIsSilent(t *testing.T) {
	s, f, j := newQQOfficialTest(t)
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		s.mu.Lock()
		s.generation++
		s.mu.Unlock()
		return nil, context.Canceled
	}
	if err := s.process(j); err != nil {
		t.Fatalf("stale job reported error: %v", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("stale job notified sender: %#v", f.sent)
	}
}

func TestQQOfficialWorkerRecordsRedactedLastError(t *testing.T) {
	s, _, j := newQQOfficialTest(t)
	s.generate = func(context.Context, sessionReplyOptions) (*generatedSessionReply, error) {
		return nil, errors.New("bad key test-secret")
	}
	go s.worker()
	s.queue <- j
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		last := s.status.LastError
		s.mu.Unlock()
		if last != "" {
			if !strings.Contains(last, "生成回复失败") || strings.Contains(last, "test-secret") {
				t.Fatalf("unexpected last error %q", last)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("last error not recorded")
}
