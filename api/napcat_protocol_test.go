package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNapCatPrivatePokeNotice(t *testing.T) {
	event := napCatMessageEvent{PostType: "notice", NoticeType: "notify", SubType: "poke", SelfID: 20002, UserID: 10001, SenderID: 10001, TargetID: 20002}
	normalized := normalizeNapCatPrivateNotice(event)
	if normalized.PostType != "message" || normalized.MessageType != "private" || normalized.UserID != 10001 {
		t.Fatalf("not normalized: %+v", normalized)
	}
	parsed, err := parseNapCatEventMessage(normalized.Message, "")
	if err != nil || parsed == nil || parsed.Text != "[戳一戳]" {
		t.Fatalf("parse: %+v %v", parsed, err)
	}
	for _, name := range []string{"group", "self", "other-target", "request"} {
		t.Run(name, func(t *testing.T) {
			bad := event
			switch name {
			case "group":
				bad.GroupID = 3
			case "self":
				bad.SenderID = bad.SelfID
			case "other-target":
				bad.TargetID = 3
			case "request":
				bad.PostType = "request"
			}
			if normalizeNapCatPrivateNotice(bad).PostType == "message" {
				t.Fatal("unexpected private input")
			}
		})
	}
}

func TestNapCatCQTextEscapesAndSticker(t *testing.T) {
	got := parseNapCatRawStringMessage("&#91;hello&#93; &amp;", true)
	if got.Text != "[hello] &" {
		t.Fatalf("text = %q", got.Text)
	}
	got = parseNapCatRawStringMessage("[CQ:mface,summary=smile,emoji_id=123]", true)
	if got.Text != "smile" {
		t.Fatalf("sticker = %q", got.Text)
	}
}

func TestNapCatResolvedImageFallback(t *testing.T) {
	path := writeTinyPNGForNapCatTest(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := httptest.NewServer(http.NotFoundHandler())
	defer unavailable.Close()
	for name, resource := range map[string]napCatGetImageData{
		"failed-url":    {URL: unavailable.URL, File: path},
		"local-url":     {URL: path, File: path},
		"remote-base64": {File: "/not-shared/image.png", Base64: base64.StdEncoding.EncodeToString(data)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := readNapCatResolvedResource(ctx, &resource, maxChatImageBodyBytes)
			if err != nil || string(got) != string(data) {
				t.Fatalf("fallback: %v", err)
			}
		})
	}
	if _, err := readNapCatResolvedResource(context.Background(), &napCatGetImageData{Base64: base64.StdEncoding.EncodeToString(data)}, 1); err == nil {
		t.Fatal("accepted oversized base64")
	}
}

func TestNapCatOutgoingLiteralCQ(t *testing.T) {
	requests := make(chan napCatActionRequest, 2)
	s := newNapCatTestService(t, nil, func(req napCatActionRequest) napCatActionResponse {
		if req.Action == "get_login_info" {
			return napCatActionResponse{Data: json.RawMessage(`{"user_id":20002}`)}
		}
		requests <- req
		return napCatActionResponse{}
	})
	if err := s.sendPrivateText(context.Background(), 10001, "literal [CQ:face,id=14]"); err != nil {
		t.Fatal(err)
	}
	req := <-requests
	params := req.Params.(map[string]interface{})
	raw, _ := json.Marshal(params["message"])
	var segments []napCatMessageSegment
	if err := json.Unmarshal(raw, &segments); err != nil || len(segments) != 1 || segments[0].Type != "text" || segments[0].Data.Text != "literal [CQ:face,id=14]" {
		t.Fatalf("not literal text: %s", raw)
	}
}
