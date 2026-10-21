package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cornerstone/storage"
)

func receiveTestMaterializer(t *testing.T, action func(context.Context, string, map[string]interface{}) (json.RawMessage, error)) *napCatReceiveMaterializer {
	t.Helper()
	store := storage.AttachmentStore{Dir: t.TempDir()}
	return &napCatReceiveMaterializer{budget: &napCatReceiveBudget{}, action: action, saveAttachment: store.Save, saveImage: func(b []byte) (string, error) {
		if len(b) == 0 {
			return "", errors.New("empty")
		}
		return "/api/cache/photo.png", nil
	}}
}
func receiveTestParse(t *testing.T, raw string) *napCatReceiveParsedMessage {
	t.Helper()
	p, e := parseNapCatReceiveMessage(json.RawMessage(raw), "", true)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestNapCatReceiveParsesMediaAndCQ(t *testing.T) {
	p := receiveTestParse(t, `[{"type":"text","data":{"text":"hello "}},{"type":"text","data":{"text":"world"}},{"type":"reply","data":{"id":"42"}},{"type":"record","data":{"file":"voice"}},{"type":"video","data":{"file":"video"}},{"type":"file","data":{"file_id":"f","file_name":"report.txt"}},{"type":"node","data":{"name":"Alice","content":[{"type":"text","data":{"text":"nested"}}]}}]`)
	if p.Text != "hello world" || p.ReplyMessageID != 42 || len(p.MediaSegments) != 4 || p.MediaSegments[2].Data.FileName != "report.txt" {
		t.Fatalf("unexpected parse: %+v", p)
	}
	p, e := parseNapCatReceiveMessage(nil, `hello[CQ:record,file=voice][CQ:file,file_id=f,name=a&#44;b.txt]`, true)
	if e != nil || len(p.MediaSegments) != 2 || p.MediaSegments[1].Data.Name != "a,b.txt" {
		t.Fatalf("CQ: %+v %v", p, e)
	}
}
func TestNapCatReceiveForwardCycleAndAttachments(t *testing.T) {
	calls := 0
	m := receiveTestMaterializer(t, func(_ context.Context, action string, params map[string]interface{}) (json.RawMessage, error) {
		switch action {
		case "get_forward_msg":
			calls++
			if params["message_id"] != "fwd" {
				t.Fatal(params)
			}
			return json.RawMessage(`{"messages":[{"sender":{"nickname":"Alice"},"message":[{"type":"text","data":{"text":"forward body"}},{"type":"file","data":{"file_id":"doc","name":"a.txt"}},{"type":"forward","data":{"id":"fwd"}}]},{"sender":{"nickname":"Bob"},"content":[{"type":"text","data":{"text":"content alias"}}]}]}`), nil
		case "get_private_file_url":
			return nil, errors.New("unavailable")
		case "get_file":
			return json.RawMessage(`{"base64":"aGVsbG8="}`), nil
		}
		t.Fatalf("unexpected action %s", action)
		return nil, nil
	})
	m.materialize(context.Background(), receiveTestParse(t, `[{"type":"forward","data":{"id":"fwd"}}]`), 0)
	if calls != 1 || len(napCatTestReceivedAttachments(m)) != 1 {
		t.Fatalf("calls=%d attachments=%v", calls, napCatTestReceivedAttachments(m))
	}
	for _, part := range []string{"Alice", "forward body", "Bob", "content alias", "循环", "未转写或读取"} {
		if !strings.Contains(napCatTestReceivedText(m), part) {
			t.Errorf("missing %q: %s", part, napCatTestReceivedText(m))
		}
	}
	if m.budget.bytes != 5 {
		t.Fatal(m.budget.bytes)
	}
}
func TestNapCatReceiveInlineForwardVariants(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"node","data":{"name":"Alice","content":[{"type":"text","data":{"text":"hello"}}]}}]`,
		`[{"type":"forward","data":{"content":[{"type":"node","data":{"nickname":"Alice","message":[{"type":"text","data":{"text":"hello"}}]}}]}}]`,
		`[{"type":"forward","data":{"content":[{"sender":{"nickname":"Alice"},"content":[{"type":"text","data":{"text":"hello"}}]}]}}]`,
	} {
		m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
			t.Fatal("inline must not resolve")
			return nil, nil
		})
		m.materialize(context.Background(), receiveTestParse(t, raw), 0)
		if !strings.Contains(napCatTestReceivedText(m), "Alice") || !strings.Contains(napCatTestReceivedText(m), "hello") {
			t.Fatal(napCatTestReceivedText(m))
		}
	}
}
func TestNapCatReceiveVoiceConversionAndFallback(t *testing.T) {
	for _, convert := range []bool{true, false} {
		var actions []string
		m := receiveTestMaterializer(t, func(_ context.Context, a string, p map[string]interface{}) (json.RawMessage, error) {
			actions = append(actions, a)
			if a == "get_record" {
				if p["out_format"] != "mp3" {
					t.Fatal(p)
				}
				if !convert {
					return nil, errors.New("codec unavailable")
				}
				return json.RawMessage(`{"base64":"SUQz"}`), nil
			}
			if a != "get_file" {
				t.Fatal(a)
			}
			return json.RawMessage(`{"base64":"IyFTSUxLX1Yz"}`), nil
		})
		m.materialize(context.Background(), receiveTestParse(t, `[{"type":"record","data":{"file":"voice"}}]`), 0)
		if len(napCatTestReceivedAttachments(m)) != 1 || napCatTestReceivedAttachments(m)[0].Kind != "audio" {
			t.Fatal(napCatTestReceivedAttachments(m))
		}
		if convert {
			if len(actions) != 1 || !strings.HasSuffix(napCatTestReceivedAttachments(m)[0].Name, ".mp3") {
				t.Fatal(actions, napCatTestReceivedAttachments(m))
			}
		} else if len(actions) != 2 || napCatTestReceivedAttachments(m)[0].MIMEType != "audio/silk" {
			t.Fatal(actions, napCatTestReceivedAttachments(m))
		}
	}
}
func TestNapCatReceiveLocalPathTrustBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if e := os.WriteFile(path, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"image", "record", "video", "file"} {
		raw, _ := json.Marshal([]map[string]interface{}{{"type": kind, "data": map[string]string{"file": path, "path": path, "url": "file://" + path}}})
		m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
			t.Fatal("event local path must not be sent to API for opening")
			return nil, nil
		})
		m.materialize(context.Background(), receiveTestParse(t, string(raw)), 0)
		if len(napCatTestReceivedAttachments(m))+len(napCatTestReceivedImages(m)) != 0 || !strings.Contains(napCatTestReceivedText(m), "接收失败") {
			t.Fatal(napCatTestReceivedText(m))
		}
	}
	// A path returned by the authenticated NapCat API is a supported shared-storage fallback.
	m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		return json.Marshal(map[string]string{"file": path})
	})
	m.materialize(context.Background(), receiveTestParse(t, `[{"type":"file","data":{"file_id":"opaque-id"}}]`), 0)
	if len(napCatTestReceivedAttachments(m)) != 1 {
		t.Fatal(napCatTestReceivedText(m))
	}
}
func TestNapCatReceiveURLAndFailedMediaPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("file bytes"))
	}))
	defer server.Close()
	m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		return nil, errors.New("offline")
	})
	raw, _ := json.Marshal([]map[string]interface{}{{"type": "file", "data": map[string]string{"url": server.URL + "/ok", "name": "../../safe.txt"}}, {"type": "video", "data": map[string]string{"url": server.URL + "/bad"}}})
	m.materialize(context.Background(), receiveTestParse(t, string(raw)), 0)
	if len(napCatTestReceivedAttachments(m)) != 1 || napCatTestReceivedAttachments(m)[0].Name != "safe.txt" || !strings.Contains(napCatTestReceivedText(m), "视频附件接收失败") {
		t.Fatal(napCatTestReceivedAttachments(m), napCatTestReceivedText(m))
	}
}
func TestNapCatReceiveSharedLimits(t *testing.T) {
	calls := 0
	m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"base64":"eA=="}`), nil
	})
	p := receiveTestParse(t, `[{"type":"file","data":{"file_id":"f"}}]`)
	for i := 0; i < 20; i++ {
		m.materialize(context.Background(), p, 0)
	}
	if calls != 16 || len(napCatTestReceivedAttachments(m)) != 16 || !strings.Contains(napCatTestReceivedText(m), "超出接收限额") {
		t.Fatal(calls, len(napCatTestReceivedAttachments(m)))
	}
	m.budget = &napCatReceiveBudget{bytes: storage.MaxAttachmentBytes - 1}
	m.chain = nil
	m.action = func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		return json.RawMessage(`{"base64":"eHg="}`), nil
	}
	m.materialize(context.Background(), p, 0)
	if len(napCatTestReceivedAttachments(m)) != 0 || m.budget.bytes != storage.MaxAttachmentBytes-1 {
		t.Fatal("exceeded byte budget")
	}
	m.appendText(strings.Repeat("字", napCatReceiveMaxText))
	if len(napCatTestReceivedText(m)) > napCatReceiveMaxText+10000 || m.budget.text > napCatReceiveMaxText {
		t.Fatal("text budget exceeded")
	}
	// Depth bound and node bound apply to all branches, not just one recursion path.
	m = receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		calls++
		return nil, errors.New("unexpected")
	})
	raw := `[{"type":"text","data":{"text":"deep leaf"}}]`
	for i := 0; i < 10; i++ {
		raw = `[{"type":"node","data":{"content":` + raw + `}}]`
	}
	m.materialize(context.Background(), receiveTestParse(t, raw), 0)
	if strings.Contains(napCatTestReceivedText(m), "deep leaf") || !strings.Contains(napCatTestReceivedText(m), "展开限额") {
		t.Fatal(napCatTestReceivedText(m))
	}
	m.budget.nodes = napCatReceiveMaxNodes
	m.materialize(context.Background(), receiveTestParse(t, `[{"type":"forward","data":{"id":"blocked"}}]`), 0)
	if m.budget.nodes > napCatReceiveMaxNodes {
		t.Fatal(m.budget.nodes)
	}
}
func TestNapCatReceiveMalformedAndSizeBounds(t *testing.T) {
	for _, raw := range []string{`{"bad":true}`, strings.Repeat("x", napCatReceiveMaxRaw+1)} {
		if _, err := parseNapCatReceiveMessage(json.RawMessage(raw), "", true); err == nil {
			t.Fatal("accepted invalid message")
		}
	}
	m := receiveTestMaterializer(t, func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		return json.RawMessage(`{"messages":false}`), nil
	})
	m.materialize(context.Background(), receiveTestParse(t, `[{"type":"forward","data":{"id":"bad"}}]`), 0)
	if !strings.Contains(napCatTestReceivedText(m), "解析失败") {
		t.Fatal(napCatTestReceivedText(m))
	}
	// Resource helper rejects base64 larger than remaining shared quota.
	m.action = func(context.Context, string, map[string]interface{}) (json.RawMessage, error) {
		return json.Marshal(map[string]string{"base64": base64.StdEncoding.EncodeToString([]byte("over budget"))})
	}
	seg := receiveTestParse(t, `[{"type":"file","data":{"file_id":"f"}}]`).MediaSegments[0]
	if _, _, err := m.readMedia(context.Background(), seg, 1); err == nil {
		t.Fatal("oversized media accepted")
	}
}
func TestNapCatReceiveQuoteAttachmentRoundTrip(t *testing.T) {
	quote := storage.QuotedMessage{Content: "引用附件", Attachments: []storage.ChatAttachment{{Path: "/api/attachments/id", Name: "a.txt", Kind: "file", Size: 1}}}
	data, e := json.Marshal(quote)
	if e != nil {
		t.Fatal(e)
	}
	var got storage.QuotedMessage
	if e = json.Unmarshal(data, &got); e != nil || len(got.Attachments) != 1 || got.Attachments[0].Name != "a.txt" {
		t.Fatal(string(data), e)
	}
}
