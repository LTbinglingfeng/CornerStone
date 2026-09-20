package client

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type ClawBotReference struct {
	MessageItem *ClawBotIncomingMessageItem `json:"message_item,omitempty"`
	Title       string                      `json:"title,omitempty"`
	ServerID    ClawBotMessageID            `json:"svr_id,omitempty"`
	PartialText *ClawBotPartialText         `json:"partial_text,omitempty"`
}

type ClawBotPartialText struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	StartIndex int    `json:"startindex"`
	EndIndex   int    `json:"endindex"`
	QuoteMD5   string `json:"quotemd5"`
}

// Preserve unknown media/protocol fields through durable queues and cache.
// A malformed optional reference must not reject the enclosing getupdates.
func (item *ClawBotIncomingMessageItem) UnmarshalJSON(data []byte) error {
	type plain ClawBotIncomingMessageItem
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	ref := fields["ref_msg"]
	msgID := fields["msg_id"]
	delete(fields, "ref_msg")
	delete(fields, "msg_id")
	body, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	var value plain
	if err = json.Unmarshal(body, &value); err != nil {
		return err
	}
	*item = ClawBotIncomingMessageItem(value)
	item.Raw = append(json.RawMessage(nil), data...)
	_ = json.Unmarshal(msgID, &item.MsgID)
	if len(ref) > 0 && string(ref) != "null" {
		var r ClawBotReference
		if json.Unmarshal(ref, &r) == nil {
			item.RefMsg = &r
		}
	}
	return nil
}

func (item ClawBotIncomingMessageItem) MarshalJSON() ([]byte, error) {
	type plain ClawBotIncomingMessageItem
	data, err := json.Marshal(plain(item))
	if err != nil {
		return nil, err
	}
	if len(item.Raw) == 0 {
		return data, nil
	}
	var original, known map[string]json.RawMessage
	if json.Unmarshal(item.Raw, &original) != nil {
		return data, nil
	}
	if err = json.Unmarshal(data, &known); err != nil {
		return nil, err
	}
	// RefMsg may have been rejected during tolerant parsing; don't reintroduce it.
	delete(original, "ref_msg")
	for k, v := range known {
		original[k] = mergeClawBotRawObject(original[k], v)
	}
	return json.Marshal(original)
}

// Overlay typed fields without losing future nested media metadata. RawMessage
// avoids float64 conversion of uint64 protocol values at every nesting level.
func mergeClawBotRawObject(original, current json.RawMessage) json.RawMessage {
	var oldFields, fields map[string]json.RawMessage
	if json.Unmarshal(original, &oldFields) != nil || oldFields == nil || json.Unmarshal(current, &fields) != nil || fields == nil {
		return current
	}
	for key, value := range fields {
		oldFields[key] = mergeClawBotRawObject(oldFields[key], value)
	}
	result, err := json.Marshal(oldFields)
	if err != nil {
		return current
	}
	return result
}

// ResolveClawBotPartialQuote implements both observed occurrence-index variants.
// Indices are occurrence counts, NOT byte, rune or UTF-16 offsets.
func ResolveClawBotPartialQuote(text string, p *ClawBotPartialText) string {
	if p == nil {
		return text
	}
	nth := func(value string, n, from int) int {
		if value == "" || n < 0 || n > len(text) {
			return -1
		}
		for i := 0; i <= n; i++ {
			pos := strings.Index(text[from:], value)
			if pos < 0 {
				return -1
			}
			pos += from
			if i == n {
				return pos
			}
			from = pos + len(value)
		}
		return -1
	}
	start := nth(p.Start, p.StartIndex, 0)
	if start < 0 {
		return text
	}
	for _, from := range []int{0, start + len(p.Start)} {
		end := nth(p.End, p.EndIndex, from)
		if end < start {
			continue
		}
		quote := text[start : end+len(p.End)]
		sum := md5.Sum([]byte(quote)) // Protocol checksum, not a security primitive.
		if p.QuoteMD5 == "" || strings.EqualFold(hex.EncodeToString(sum[:]), p.QuoteMD5) {
			return quote
		}
	}
	return text
}

// RestoreClawBotReferences expands references before normal attachment parsing.
// It never recursively expands quoted references, and never changes the input.
// Lookup receives a lossless server ID and returns the original raw item list.
func RestoreClawBotReferences(msg ClawBotIncomingMessage, lookup func(ClawBotMessageID) json.RawMessage) ClawBotIncomingMessage {
	out := msg
	out.ItemList = append([]ClawBotIncomingMessageItem(nil), msg.ItemList...)
	var quotes []string
	var media []ClawBotIncomingMessageItem
	for _, item := range msg.ItemList {
		ref := item.RefMsg
		if ref == nil {
			continue
		}
		id := ref.ServerID
		if id == "" && ref.MessageItem != nil {
			id = ref.MessageItem.MsgID
		}
		var quoted []ClawBotIncomingMessageItem
		if ref.MessageItem != nil {
			quoted = []ClawBotIncomingMessageItem{*ref.MessageItem}
		}
		if len(quoted) == 0 && id != "" && lookup != nil {
			_ = json.Unmarshal(lookup(id), &quoted)
		}
		parts := []string{}
		if ref.Title != "" {
			parts = append(parts, ref.Title)
		}
		for _, q := range quoted {
			if q.Type == 1 && q.TextItem != nil {
				parts = append(parts, ResolveClawBotPartialQuote(q.TextItem.Text, ref.PartialText))
			} else if q.Type >= 2 && q.Type <= 5 {
				q.RefMsg = nil
				q.IsReference = true
				media = append(media, q)
				label := map[int]string{2: "图片", 3: "语音", 4: "文件", 5: "视频"}[q.Type]
				parts = append(parts, "[引用"+label+"]")
			}
		}
		if len(parts) == 0 {
			parts = append(parts, "引用消息内容未缓存")
		}
		quotes = append(quotes, "[引用消息]\n"+strings.Join(parts, "\n")+"\n[/引用消息]")
	}
	if len(quotes) == 0 {
		return out
	}
	// ExtractText historically returns only the first text/voice item. Prepending
	// one synthesized text preserves that contract and keeps commands in quotes
	// from being interpreted as commands from the current user.
	text := ExtractTextFromClawBotMessage(msg)
	if text != "" {
		text += "\n\n"
	}
	text += strings.Join(quotes, "\n")
	out.ItemList = append([]ClawBotIncomingMessageItem{{Type: 1, TextItem: &ClawBotItemText{Text: text}}}, out.ItemList...)
	out.ItemList = append(out.ItemList, media...)
	return out
}

func HasClawBotReference(msg ClawBotIncomingMessage) bool {
	for _, item := range msg.ItemList {
		if item.RefMsg != nil {
			return true
		}
	}
	return false
}

// IsClawBotReferencedImage identifies synthetic quote attachments so failed CDN
// restoration can degrade to text without parking the current inbound message.
func IsClawBotReferencedImage(msg ClawBotIncomingMessage, image *ClawBotImageItem) bool {
	for _, item := range msg.ItemList {
		if item.IsReference && item.ImageItem == image {
			return true
		}
	}
	return false
}
