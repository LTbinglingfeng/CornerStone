package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClawBotReferenceLosslessAndRaw(t *testing.T) {
	var msg ClawBotIncomingMessage
	if err := json.Unmarshal([]byte(`{"message_id":18446744073709551615,"session_id":"s","item_list":[{"type":1,"text_item":{"text":"question"},"ref_msg":{"svr_id":18446744073709551614}},{"type":5,"future_media":{"n":18446744073709551615}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MessageID != "18446744073709551615" || msg.ItemList[0].RefMsg.ServerID != "18446744073709551614" {
		t.Fatal(msg)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"n":18446744073709551615`) {
		t.Fatal(string(raw))
	}
	restored := RestoreClawBotReferences(msg, func(id ClawBotMessageID) json.RawMessage {
		if id != "18446744073709551614" {
			t.Fatal(id)
		}
		return json.RawMessage(`[{"type":1,"text_item":{"text":"original"}},{"type":2,"image_item":{"media":{"full_url":"https://example.invalid/image"}}}]`)
	})
	if text := ExtractTextFromClawBotMessage(restored); !strings.Contains(text, "original") || !strings.Contains(text, "question") {
		t.Fatal(text)
	}
	if len(ExtractImageItemsFromClawBotMessage(restored)) != 1 {
		t.Fatal("attachment not restored")
	}
	if len(msg.ItemList) != 2 || msg.ItemList[0].TextItem.Text != "question" {
		t.Fatal("input mutated")
	}
}

func TestClawBotReferenceMalformedAndMissing(t *testing.T) {
	for _, ref := range []string{`{"svr_id":{}}`, `{"partial_text":{"startindex":"bad"}}`, `"invalid"`} {
		var item ClawBotIncomingMessageItem
		if err := json.Unmarshal([]byte(`{"type":1,"text_item":{"text":"still received"},"ref_msg":`+ref+`}`), &item); err != nil {
			t.Fatal(err)
		}
		if item.RefMsg != nil || item.TextItem.Text != "still received" {
			t.Fatal(item)
		}
	}
	msg := ClawBotIncomingMessage{ItemList: []ClawBotIncomingMessageItem{{Type: 1, RefMsg: &ClawBotReference{ServerID: "10"}}}}
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`broken`), json.RawMessage(`{}`)} {
		out := RestoreClawBotReferences(msg, func(ClawBotMessageID) json.RawMessage { return raw })
		if !strings.Contains(ExtractTextFromClawBotMessage(out), "未缓存") {
			t.Fatal(out)
		}
	}
}

func TestClawBotPartialReference(t *testing.T) {
	text := "前😀A尾 中 A选中尾 后"
	cases := []struct {
		p    ClawBotPartialText
		want string
	}{
		{ClawBotPartialText{Start: "A", End: "尾", StartIndex: 1, EndIndex: 1}, "A选中尾"},
		{ClawBotPartialText{Start: "😀", End: "尾"}, "😀A尾"},
		{ClawBotPartialText{Start: "A", End: "尾", QuoteMD5: "bad"}, text},
		{ClawBotPartialText{Start: "A", End: "尾", StartIndex: -1}, text},
		{ClawBotPartialText{Start: "not found", End: "尾"}, text},
	}
	for _, c := range cases {
		if got := ResolveClawBotPartialQuote(text, &c.p); got != c.want {
			t.Fatalf("got %q want %q", got, c.want)
		}
	}
	// Relative end-index interpretation selected by MD5.
	p := ClawBotPartialText{Start: "b", End: "e", StartIndex: 0, EndIndex: 0, QuoteMD5: "00000000000000000000000000000000"}
	// Invalid checksum must safely retain the full source.
	if got := ResolveClawBotPartialQuote("e before b e", &p); got != "e before b e" {
		t.Fatal(got)
	}
}
