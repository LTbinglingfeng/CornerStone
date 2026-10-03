package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNapCatForwardPreservesInlineOrder(t *testing.T) {
	m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		t.Fatal("unexpected API")
		return nil, nil
	})
	p := receiveTestParse(t, `[{"type":"text","data":{"text":"BEFORE"}},{"type":"node","data":{"name":"Alice","content":[{"type":"text","data":{"text":"INNER_BEFORE"}},{"type":"node","data":{"name":"Bob","content":[{"type":"text","data":{"text":"BODY"}}]}},{"type":"text","data":{"text":"INNER_AFTER"}}]}},{"type":"text","data":{"text":"AFTER"}}]`)
	m.materialize(context.Background(), p, 0)
	text := m.text.String()
	last := -1
	for _, part := range []string{"BEFORE", "Alice", "INNER_BEFORE", "Bob", "BODY", "INNER_AFTER", "\n[合并转发结束]\nAFTER"} {
		next := strings.Index(text, part)
		if next <= last {
			t.Fatalf("wrong order for %s: %s", part, text)
		}
		last = next
	}
}

func TestNapCatImageHTMLSuccessResponseStillFallsBack(t *testing.T) {
	path := writeTinyPNGForNapCatTest(t)
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>expired</html>"))
	}))
	defer html.Close()
	for _, direct := range []bool{true, false} {
		calls := 0
		m := receiveTestMaterializer(t, func(_ context.Context, action string, _ map[string]interface{}) (json.RawMessage, error) {
			calls++
			if action != "get_image" {
				t.Fatalf("unexpected %s", action)
			}
			// An invalid base64/URL representation must not mask a valid local file.
			return json.Marshal(napCatGetImageData{Base64: "PGh0bWw+YmFkPC9odG1sPg==", URL: html.URL, File: path})
		})
		data := map[string]string{"file": "image-id"}
		if direct {
			data["url"] = html.URL
		}
		raw, _ := json.Marshal([]map[string]interface{}{{"type": "image", "data": data}})
		m.materialize(context.Background(), receiveTestParse(t, string(raw)), 0)
		if calls != 1 || len(m.images) != 1 || strings.Contains(m.text.String(), "失败") {
			t.Fatalf("calls=%d images=%v text=%s", calls, m.images, m.text.String())
		}
	}
}

func TestNapCatMediaDeadlinePreservesTextAndCompletedAttachments(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("saved file"))
	}))
	defer media.Close()
	h := newNapCatQuoteTestHandler(t)
	s := newNapCatTestService(t, h, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "get_login_info" {
			return napCatActionResponse{Data: json.RawMessage(`{"user_id":20002}`)}
		}
		if req.Action != "send_private_msg" {
			t.Errorf("unexpected action: %s", req.Action)
		}
		return napCatActionResponse{}
	})
	raw, _ := json.Marshal([]map[string]interface{}{
		{"type": "text", "data": map[string]string{"text": "BEFORE"}},
		{"type": "file", "data": map[string]string{"url": media.URL + "/file", "name": "saved.txt"}},
		{"type": "image", "data": map[string]string{"url": media.URL + "/slow"}},
		{"type": "text", "data": map[string]string{"text": "AFTER"}},
	})
	source := napCatChatSource{Kind: "private", SelfID: 20002, UserID: 10001}
	pending, ok := s.parseIncomingPrivateMessage(source, napCatMessageEvent{MessageID: 1, Message: raw})
	if !ok {
		t.Fatal("input rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.processIncomingBatch(ctx, h.configManager.GetNapCatConfig(), source, []napCatPendingMessage{*pending})
	if ctx.Err() != nil {
		t.Fatal("media consumed entire processing deadline")
	}
	sessions := h.chatManager.ListSessions()
	if len(sessions) != 1 {
		t.Fatal(sessions)
	}
	record, _ := h.chatManager.GetSession(sessions[0].ID)
	if len(record.Messages) == 0 {
		t.Fatal("dropped entire inbound batch")
	}
	message := record.Messages[0]
	if len(message.Attachments) != 1 || !strings.Contains(message.Content, "BEFORE") || !strings.Contains(message.Content, "AFTER") || !strings.Contains(message.Content, "失败") {
		t.Fatalf("lost partial input: %+v", message)
	}
}
