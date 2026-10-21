package api

import (
	"context"
	"cornerstone/storage"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNapCatOrderedChainConfirmedPrefix(t *testing.T) {
	for _, fail := range []int{0, 2, 3} {
		t.Run(string(rune('0'+fail)), func(t *testing.T) {
			var requests []string
			s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse {
				b, _ := json.Marshal(r.Params)
				requests = append(requests, string(b))
				if len(requests) == fail {
					return &napCatActionResponse{Status: "failed", RetCode: 1}
				}
				return &napCatActionResponse{Status: "ok"}
			})
			if err := os.WriteFile(filepath.Join(s.handler.cachePhotoDir, "p.png"), []byte("image"), 0600); err != nil {
				t.Fatal(err)
			}
			a := storage.ChatAttachment{Path: "cache_photo/p.png", Kind: "image"}
			chain := napCatMessageChain{{Text: "before [CQ:image,file=secret]"}, {Attachment: &a}, {Text: "after"}}
			confirmed, err := s.deliverNapCatChain(context.Background(), 42, chain)
			want := 3
			if fail != 0 {
				want = fail - 1
			}
			if len(confirmed) != want || (err != nil) != (fail != 0) {
				t.Fatalf("confirmed=%v err=%v", confirmed, err)
			}
			sent := 3
			if fail != 0 {
				sent = fail
			}
			if len(requests) != sent || !strings.Contains(requests[0], `"type":"text"`) || !strings.Contains(requests[0], "[CQ:image,file=secret]") {
				t.Fatal(requests)
			}
			if sent >= 2 && !strings.Contains(requests[1], `"type":"image"`) {
				t.Fatal(requests)
			}
			if sent == 3 && !strings.Contains(requests[2], `"text":"after"`) {
				t.Fatal(requests)
			}
		})
	}
}

func TestNapCatOrderedReplyHistoryStopsAtUnsafeResource(t *testing.T) {
	calls := 0
	s := napCatDeliveryTestService(t, func(r napCatActionRequest) *napCatActionResponse { calls++; return &napCatActionResponse{Status: "ok"} })
	a := storage.ChatAttachment{Path: filepath.Join(t.TempDir(), "secret"), Kind: "file"}
	reply := &napCatGeneratedReply{Chain: napCatMessageChain{{Text: "confirmed"}, {Attachment: &a}, {Text: "not sent"}}, Text: "legacy must not send"}
	// Use the real reply/history path, not just the encoder.
	if _, err := s.handler.chatManager.CreateSession("chain", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "private", UserID: 42}, "chain", reply); err == nil {
		t.Fatal("unsafe resource accepted")
	}
	if calls != 1 {
		t.Fatalf("sent %d", calls)
	}
	record, ok := s.handler.chatManager.GetSession("chain")
	if !ok {
		t.Fatal("missing session")
	}
	b, _ := json.Marshal(record)
	if !strings.Contains(string(b), "confirmed") || strings.Contains(string(b), "not sent") || strings.Contains(string(b), "legacy must not send") || strings.Contains(string(b), a.Path) {
		t.Fatal(string(b))
	}
}

func TestNapCatLegacyChainDoesNotInventOrdering(t *testing.T) {
	reply := &napCatGeneratedReply{Text: "before → after", StorageMessages: []storage.ChatMessage{{Role: "assistant", Content: "before", ImagePaths: []string{"cache_photo/a.png"}}, {Role: "assistant", Content: "after"}}}
	chain := legacyNapCatReplyChain(reply, "→")
	if len(chain) != 3 || chain[0].Attachment == nil || chain[1].Text != "before" || chain[2].Text != "after" {
		t.Fatal(chain)
	}
}

func TestNapCatReceiveChainKeepsOrderAndFailurePlaceholder(t *testing.T) {
	p, err := parseNapCatReceiveMessage(json.RawMessage(`[{"type":"text","data":{"text":"before"}},{"type":"image","data":{"file":"/untrusted/image"}},{"type":"text","data":{"text":"after"}}]`), "", false)
	if err != nil {
		t.Fatal(err)
	}
	m := &napCatReceiveMaterializer{budget: &napCatReceiveBudget{}}
	m.materialize(context.Background(), p, 0)
	text, images, attachments := m.chain.legacyReceived()
	if len(images)+len(attachments) != 0 || !strings.HasPrefix(text, "before") || !strings.HasSuffix(text, "after") || !strings.Contains(text, "接收失败") {
		t.Fatal(text)
	}
	if len(m.chain) < 3 {
		t.Fatal(m.chain)
	}
}

// Existing receive behavior assertions exercise the explicit legacy projection.
func napCatTestReceivedText(m *napCatReceiveMaterializer) string {
	text, _, _ := m.chain.legacyReceived()
	return text
}
func napCatTestReceivedImages(m *napCatReceiveMaterializer) []string {
	_, images, _ := m.chain.legacyReceived()
	return images
}
func napCatTestReceivedAttachments(m *napCatReceiveMaterializer) []storage.ChatAttachment {
	_, _, attachments := m.chain.legacyReceived()
	return attachments
}

func TestNapCatReceiveResolvedMixedChain(t *testing.T) {
	image := writeTinyPNGForNapCatTest(t)
	m := receiveTestMaterializer(t, func(_ context.Context, action string, _ map[string]interface{}) (json.RawMessage, error) {
		if action != "get_image" {
			t.Fatalf("unexpected action %s", action)
		}
		return json.Marshal(napCatGetImageData{File: image})
	})
	m.materialize(context.Background(), receiveTestParse(t, `[{"type":"text","data":{"text":"before"}},{"type":"image","data":{"file":"opaque-id"}},{"type":"text","data":{"text":"after"}}]`), 0)
	if len(m.chain) != 4 || m.chain[0].Text != "before" || m.chain[1].Attachment == nil || m.chain[1].Attachment.Kind != "image" || !strings.Contains(m.chain[2].Text, "用户发送了图片") || m.chain[3].Text != "after" {
		t.Fatalf("chain: %+v", m.chain)
	}
}

func TestNapCatOrderedReplyPersistsConfirmedMediaOnly(t *testing.T) {
	calls := 0
	s := napCatDeliveryTestService(t, func(napCatActionRequest) *napCatActionResponse {
		calls++
		if calls == 3 {
			return &napCatActionResponse{Status: "failed", RetCode: 1}
		}
		return &napCatActionResponse{Status: "ok"}
	})
	if _, err := s.handler.chatManager.CreateSession("ordered", "test", "", ""); err != nil {
		t.Fatal(err)
	}
	a := storage.ChatAttachment{Path: "cache_photo/photo.png", Kind: "image"}
	if err := os.WriteFile(filepath.Join(s.handler.cachePhotoDir, "photo.png"), []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	reply := &napCatGeneratedReply{Chain: napCatMessageChain{{Text: "before"}, {Attachment: &a}, {Text: "after"}, {Text: "never"}}}
	if err := s.sendGeneratedPrivateReply(context.Background(), napCatChatSource{Kind: "private", UserID: 42}, "ordered", reply); err == nil {
		t.Fatal("expected failure")
	}
	record, _ := s.handler.chatManager.GetSession("ordered")
	found := false
	for _, m := range record.Messages {
		if m.Role == "assistant" {
			if m.Content != "before" || len(m.ImagePaths) != 1 || m.ImagePaths[0] != a.Path {
				t.Fatalf("history: %+v", m)
			}
			found = true
		}
	}
	if !found || calls != 3 {
		t.Fatalf("found=%v calls=%d", found, calls)
	}
}
