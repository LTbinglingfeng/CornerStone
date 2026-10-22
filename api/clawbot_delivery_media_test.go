package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type clawBotMediaFixtureTransport func(*http.Request) (*http.Response, error)

func (f clawBotMediaFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type clawBotMediaFixture struct {
	mu         sync.Mutex
	items      []client.ClawBotIncomingMessageItem
	ids        []string
	uploads    int
	failUpload bool
	uncertain  bool
	uploadHook func()
}

func mediaDeliveryFixture(t *testing.T) (*ClawBotService, config.ClawBotConfig, *clawBotMediaFixture) {
	t.Helper()
	fixture := &clawBotMediaFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			if fixture.failUpload {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"ret":0,"upload_full_url":"https://cdn.test/upload"}`))
		case "/upload":
			if r.Header.Get("Authorization") != "" {
				t.Error("bot token leaked to CDN")
			}
			fixture.uploads++
			if fixture.uploadHook != nil {
				fixture.uploadHook()
			}
			w.Header().Set("x-encrypted-param", "secret-download-param")
		case "/ilink/bot/sendmessage":
			var req struct {
				Msg struct {
					ClientID string                              `json:"client_id"`
					ItemList []client.ClawBotIncomingMessageItem `json:"item_list"`
				} `json:"msg"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Msg.ItemList) != 1 {
				t.Errorf("item count=%d", len(req.Msg.ItemList))
				return
			}
			fixture.items = append(fixture.items, req.Msg.ItemList[0])
			fixture.ids = append(fixture.ids, req.Msg.ClientID)
			if fixture.uncertain {
				w.WriteHeader(502)
				return
			}
			_, _ = fmt.Fprintf(w, `{"ret":0,"message_id":"%d"}`, 100+len(fixture.items))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	s, cfg, _ := deliveryTestService(t, server.URL)
	root := t.TempDir()
	s.handler.cachePhotoDir = filepath.Join(root, "cache_photo")
	s.handler.ttsAudioDir = filepath.Join(root, "tts_audio")
	if err := os.MkdirAll(s.handler.cachePhotoDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.handler.ttsAudioDir, 0700); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(server.URL)
	hc := &http.Client{Transport: clawBotMediaFixtureTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme = base.Scheme
		clone.URL.Host = base.Host
		return http.DefaultTransport.RoundTrip(clone)
	})}
	s.client = client.NewClawBotClientWithHTTPClient(hc)
	return s, cfg, fixture
}
func stageMediaReply(t *testing.T, s *ClawBotService, cfg config.ClawBotConfig, text string, messages []storage.ChatMessage) storage.ClawBotDeliveryJob {
	t.Helper()
	j := newDeliveryJob("chat", "user", time.Now())
	j.Status = "generating"
	record, err := s.handler.chatManager.CreateSession(generateID(), "media", "", "")
	if err != nil {
		t.Fatal(err)
	}
	deliveryTestInsert(t, s, cfg, j)
	if err = s.stageDeliveryReply(cfg, j.ID, record.SessionID, &clawBotGeneratedReply{Text: text, StorageMessages: messages}, false, nil); err != nil {
		t.Fatal(err)
	}
	return deliveryTestJob(t, s, j.ID)
}
func fixtureAttachment(t *testing.T, s *ClawBotService) storage.ChatAttachment {
	t.Helper()
	a, err := s.handler.attachmentStore().Save([]byte("original file"), "test.txt", "file")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func mediaDrive(t *testing.T, s *ClawBotService, cfg config.ClawBotConfig, j storage.ClawBotDeliveryJob) storage.ClawBotDeliveryJob {
	t.Helper()
	for n := 0; n < 30; n++ {
		if deliveryTerminal(j.Status) || j.Status == "blocked" || j.Status == "uncertain" {
			return j
		}
		j = deliveryTestStep(t, s, cfg, j.ID)
	}
	t.Fatal("did not settle")
	return j
}

func TestClawBotDeliveryMixedMediaOrderAndHistory(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=")
	image, err := s.handler.saveCachePhotoBytes(png)
	if err != nil {
		t.Fatal(err)
	}
	// A minimal ISO BMFF header is sufficient for protocol fixture type sniffing.
	videoBytes := append([]byte{0, 0, 0, 24}, []byte("ftypmp42\x00\x00\x00\x00mp42isom")...)
	video, err := s.handler.attachmentStore().Save(videoBytes, "clip.mp4", "video")
	if err != nil {
		t.Fatal(err)
	}
	toolJSON, _ := json.Marshal(map[string]any{"ok": true, "tool": "send_attachment", "data": map[string]any{"attachment": a}})
	j := stageMediaReply(t, s, cfg, "caption", []storage.ChatMessage{{Role: "tool", ToolCallID: "file-call", Content: string(toolJSON)}, {Role: "assistant", Content: "caption", ImagePaths: []string{image}, Attachments: []storage.ChatAttachment{video}, Timestamp: time.Now()}})
	if len(j.Chunks) != 4 {
		t.Fatalf("chunks=%+v", j.Chunks)
	}
	j = deliveryTestStep(t, s, cfg, j.ID) // upload file; no send
	if j.NextChunk != 0 || j.Chunks[0].Media.Uploaded == nil || len(f.items) != 0 {
		t.Fatalf("upload counted as delivery: %+v", j)
	}
	public, _ := json.Marshal(s.deliveryList())
	for _, secret := range []string{"secret-download-param", "aes_key", "blob_id", "source_path"} {
		if strings.Contains(string(public), secret) {
			t.Fatalf("public queue leaks %s", secret)
		}
	}
	j = deliveryTestStep(t, s, cfg, j.ID) // acknowledge file only
	j = deliveryTestStep(t, s, cfg, j.ID) // commit partial
	history, _ := s.handler.chatManager.GetSession(j.SessionID)
	if len(history.Messages) != 2 || history.Messages[1].Role != "assistant" || len(history.Messages[1].Attachments) != 1 || history.Messages[1].Attachments[0].Path != j.Chunks[0].Media.HistoryPath {
		t.Fatalf("partial history contains unsent media/text: %+v", history.Messages)
	}
	j = mediaDrive(t, s, cfg, j)
	if j.Status != "done" || f.uploads != 3 {
		t.Fatalf("job=%+v uploads=%d", j, f.uploads)
	}
	got := []int{}
	for _, item := range f.items {
		got = append(got, item.Type)
	}
	if !reflect.DeepEqual(got, []int{4, 1, 2, 5}) {
		t.Fatalf("order=%v", got)
	}
	if f.items[0].FileItem.Len != "13" || f.items[2].ImageItem.MidSize%16 != 0 || f.items[3].VideoItem.VideoSize%16 != 0 {
		t.Fatalf("wrong media wire sizes: %+v", f.items)
	}
	for i, id := range f.ids {
		if id != j.Chunks[i].ClientID {
			t.Fatal("client ID not stable")
		}
	}
	history, _ = s.handler.chatManager.GetSession(j.SessionID)
	if len(history.Messages) != 3 || len(history.Messages[2].ImagePaths) != 1 || len(history.Messages[2].Attachments) != 1 {
		t.Fatalf("history=%+v", history.Messages)
	}
}

// A failed upload sent nothing for that item: the remaining segments are
// cancelled automatically, while already-sent text is kept in history.
func TestClawBotDeliveryUploadFailureCancelsRemaining(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	f.failUpload = true
	j := stageMediaReply(t, s, cfg, "first", []storage.ChatMessage{{Role: "assistant", Content: "first", Attachments: []storage.ChatAttachment{a}}})
	j = mediaDrive(t, s, cfg, j)
	if j.Status != "cancelled" || j.NextChunk != 1 || len(f.items) != 1 || !j.HistoryCommitted || j.Chunks[1].Status != "pending" {
		t.Fatalf("job=%+v", j)
	}
	history, _ := s.handler.chatManager.GetSession(j.SessionID)
	last := history.Messages[len(history.Messages)-1]
	if last.Role != "assistant" || last.Content != "first" || len(last.Attachments) != 0 {
		t.Fatalf("history contains unsent media: %+v", history.Messages)
	}
}

// An unknown media send outcome counts as delivered without resending; a
// later history failure finalizes the job instead of waiting for a human.
func TestClawBotDeliveryMediaUnknownTreatedAsDeliveredAndHistoryFailure(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	j := stageMediaReply(t, s, cfg, "", []storage.ChatMessage{{Role: "assistant", Attachments: []storage.ChatAttachment{a}}})
	j = deliveryTestStep(t, s, cfg, j.ID)
	f.uncertain = true
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Status != "committing" || j.NextChunk != 1 || j.Chunks[0].Status != "sent" || f.uploads != 1 {
		t.Fatalf("job=%+v", j)
	}
	if err := s.handler.chatManager.DeleteSession(j.SessionID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3 && !deliveryTerminal(j.Status); i++ {
		j = deliveryTestStep(t, s, cfg, j.ID)
	}
	if j.Status != "cancelled" || j.HistoryCommitted || j.Chunks[0].Media.Uploaded == nil {
		t.Fatalf("audit lost: %+v", j)
	}
	if len(f.items) != 1 || f.uploads != 1 {
		t.Fatal("history failure resent acknowledged media")
	}
}

func TestClawBotDeliveryMediaRecoveryAndCancelUpload(t *testing.T) {
	s, cfg, f := mediaDeliveryFixture(t)
	a := fixtureAttachment(t, s)
	j := stageMediaReply(t, s, cfg, "", []storage.ChatMessage{{Role: "assistant", Attachments: []storage.ChatAttachment{a}}})
	// A process crash during upload is not an uncertain message send.
	if err := s.changeDelivery(cfg, j.ID, func(j *storage.ClawBotDeliveryJob) error {
		j.Status = "uploading"
		j.Phase = "upload"
		j.Chunks[0].Status = "uploading"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverDeliveries(cfg); err != nil {
		t.Fatal(err)
	}
	j = deliveryTestJob(t, s, j.ID)
	if j.Status != "ready" {
		t.Fatal(j.Status)
	}
	// Account changes reject stale operator intentions.
	wrongAccount := deliveryTestAction(s, cfg, j, "cancel", "wrong-account-cancel")
	wrongAccount.AccountID = "other"
	if err := s.actOnDelivery(cfg, wrongAccount); err == nil {
		t.Fatal("cross-account action accepted")
	}
	f.uploadHook = func() {
		current := deliveryTestJob(t, s, j.ID)
		if err := s.actOnDelivery(cfg, deliveryTestAction(s, cfg, current, "cancel", "cancel-upload")); err != nil {
			t.Error(err)
		}
	}
	j = deliveryTestStep(t, s, cfg, j.ID)
	if j.Status != "cancelled" || len(f.items) != 0 || j.Chunks[0].Media.Uploaded != nil {
		t.Fatalf("cancelled upload resumed: %+v", j)
	}
}

func TestClawBotDeliveryOutgoingPathGuards(t *testing.T) {
	s, _, _ := mediaDeliveryFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("secret"), 0600)
	link := filepath.Join(s.handler.cachePhotoDir, "link.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, "cache_photo/../secret", "cache_photo/link.png", "http://example/file", "tts_audio/../../secret"} {
		if _, err := s.handler.readClawBotOutgoingMedia(&storage.ClawBotDeliveryMedia{SourcePath: p, Name: "test"}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if _, err := outgoingClawBotMediaType(&storage.ClawBotDeliveryMedia{Kind: "image", Name: "x.png"}, []byte("not image")); err == nil {
		t.Fatal("accepted mislabeled image")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.client.UploadMedia(ctx, "http://127.0.0.1", "", "", "", 1, []byte("test")); err == nil {
		t.Fatal("cancel ignored")
	}
}
