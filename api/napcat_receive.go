package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"cornerstone/storage"
)

const (
	napCatReceiveMaxRaw   = 2 << 20
	napCatReceiveMaxText  = 64 << 10
	napCatReceiveMaxNodes = 128
	napCatReceiveMaxDepth = 4
	napCatReceiveMaxMedia = 16
)

// Separate wire type: event-local paths are deliberately never read.
type napCatIncomingSegment struct {
	Type string `json:"type"`
	Data struct {
		napCatMessageSegmentData
		Content  json.RawMessage `json:"content,omitempty"`
		Message  json.RawMessage `json:"message,omitempty"`
		Name     string          `json:"name,omitempty"`
		Nickname string          `json:"nickname,omitempty"`
		FileName string          `json:"file_name,omitempty"`
		Markdown string          `json:"markdown,omitempty"`
		QQ       interface{}     `json:"qq,omitempty"`
		Result   interface{}     `json:"result,omitempty"`
	} `json:"data"`
}

type napCatReceiveParsedMessage struct {
	Segments       []napCatIncomingSegment
	Text           string
	ReplyMessageID int64
	ImageSegments  []napCatMessageSegment
	MediaSegments  []napCatIncomingSegment
}

// Parsing does not resolve remote resources; pending messages remain cheap to queue.
func parseNapCatReceiveMessage(raw json.RawMessage, fallback string, allowReply bool) (*napCatReceiveParsedMessage, error) {
	segments, err := decodeNapCatIncomingSegments(raw, fallback)
	if err != nil {
		return nil, err
	}
	return parseNapCatReceiveSegments(segments, allowReply), nil
}

func parseNapCatReceiveSegments(segments []napCatIncomingSegment, allowReply bool) *napCatReceiveParsedMessage {
	p := &napCatReceiveParsedMessage{Segments: segments}
	var text strings.Builder
	for _, seg := range segments {
		switch seg.Type {
		case "text":
			text.WriteString(seg.Data.Text)
		case "record", "video", "file", "forward", "node":
			p.MediaSegments = append(p.MediaSegments, seg)
		case "at":
			text.WriteString("[提及QQ: " + stringifyNapCatSegmentValue(seg.Data.QQ) + "]")
		case "dice", "rps":
			text.WriteString("[" + seg.Type + ": " + stringifyNapCatSegmentValue(seg.Data.Result) + "]")
		case "markdown":
			content := seg.Data.Markdown
			if content == "" {
				_ = json.Unmarshal(seg.Data.Content, &content)
			}
			text.WriteString(content)
		default:
			old := parseNapCatSegments([]napCatMessageSegment{{Type: seg.Type, Data: seg.Data.napCatMessageSegmentData}}, allowReply)
			if old == nil {
				// Unknown segments are not silently represented as understood.
				if seg.Type != "reply" {
					text.WriteString("[未解析的QQ消息段: " + napCatReceiveName(seg.Type, "unknown") + "]")
				}
				continue
			}
			text.WriteString(old.Text)
			p.ImageSegments = append(p.ImageSegments, old.ImageSegments...)
			if p.ReplyMessageID == 0 {
				p.ReplyMessageID = old.ReplyMessageID
			}
		}
	}
	p.Text = text.String()
	if p.Text == "" && len(p.ImageSegments)+len(p.MediaSegments) == 0 && p.ReplyMessageID == 0 {
		return nil
	}
	return p
}

// Keep time for persisting the input and generating a reply after slow media.
// Connection cancellation and permission checks still use the parent context.
func napCatReceiveContext(ctx context.Context) (context.Context, context.CancelFunc) {
	delay := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining < delay {
			delay = remaining
		}
	}
	return context.WithTimeout(ctx, delay)
}

func decodeNapCatIncomingSegments(raw json.RawMessage, fallback string) ([]napCatIncomingSegment, error) {
	if len(raw) > napCatReceiveMaxRaw || len(fallback) > napCatReceiveMaxRaw {
		return nil, errors.New("NapCat message exceeds receive limit")
	}
	var segments []napCatIncomingSegment
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if json.Unmarshal(raw, &segments) == nil {
			if len(segments) > napCatReceiveMaxNodes {
				return nil, errors.New("too many message segments")
			}
			return segments, nil
		}
		if json.Unmarshal(raw, &fallback) != nil {
			return nil, errors.New("invalid NapCat message")
		}
	}
	// CQ strings share the same bounded media/forward path as array messages.
	for len(fallback) > 0 {
		if len(segments) >= napCatReceiveMaxNodes {
			return nil, errors.New("too many CQ segments")
		}
		start := strings.Index(fallback, "[CQ:")
		if start < 0 {
			start = len(fallback)
		}
		if start > 0 {
			var s napCatIncomingSegment
			s.Type = "text"
			s.Data.Text = decodeNapCatCQText(fallback[:start])
			segments = append(segments, s)
			fallback = fallback[start:]
			continue
		}
		end := strings.IndexByte(fallback, ']')
		if end < 0 {
			var s napCatIncomingSegment
			s.Type = "text"
			s.Data.Text = fallback
			segments = append(segments, s)
			break
		}
		cq := fallback[4:end]
		kind, args, _ := strings.Cut(cq, ",")
		values := parseNapCatCQArgs(args)
		data := map[string]interface{}{}
		for k, v := range values {
			data[k] = v
		}
		wire, _ := json.Marshal(map[string]interface{}{"type": kind, "data": data})
		var s napCatIncomingSegment
		_ = json.Unmarshal(wire, &s)
		segments = append(segments, s)
		fallback = fallback[end+1:]
	}
	return segments, nil
}

// One budget should be reused for the entire incoming batch, including its quote.
type napCatReceiveBudget struct {
	nodes, media, text int
	bytes              int64
	seen               map[string]bool
}

type napCatReceiveMaterializer struct {
	action         func(context.Context, string, map[string]interface{}) (json.RawMessage, error)
	saveImage      func([]byte) (string, error)
	saveAttachment func([]byte, string, string) (storage.ChatAttachment, error)
	budget         *napCatReceiveBudget
	text           strings.Builder
	images         []string
	attachments    []storage.ChatAttachment
}

func (s *NapCatService) materializeNapCatReceivedMessage(ctx context.Context, parsed *napCatReceiveParsedMessage, budget *napCatReceiveBudget) (string, []string, []storage.ChatAttachment) {
	if budget == nil {
		budget = &napCatReceiveBudget{}
	}
	m := &napCatReceiveMaterializer{budget: budget, saveImage: s.handler.saveCachePhotoBytes, saveAttachment: s.handler.attachmentStore().Save}
	m.action = func(ctx context.Context, action string, params map[string]interface{}) (json.RawMessage, error) {
		r, e := s.sendActionWithEcho(ctx, action, params)
		if e != nil {
			return nil, e
		}
		if r == nil || r.Status != "ok" || r.RetCode != 0 {
			return nil, errors.New("NapCat resource action failed")
		}
		if len(r.Data) > napCatReceiveMaxRaw && action == "get_forward_msg" {
			return nil, errors.New("forward response exceeds limit")
		}
		return r.Data, nil
	}
	m.materialize(ctx, parsed, 0)
	return strings.TrimSpace(m.text.String()), m.images, m.attachments
}

func (m *napCatReceiveMaterializer) appendText(text string) {
	remaining := napCatReceiveMaxText - m.budget.text
	if remaining <= 0 {
		return
	}
	if len(text) > remaining {
		marker := "\n[文本超出接收限额，已截断]"
		suffix := ""
		if remaining >= len(marker) {
			remaining -= len(marker)
			suffix = marker
		}
		text = text[:remaining]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		text += suffix
	}
	m.text.WriteString(text)
	m.budget.text += len(text)
}
func (m *napCatReceiveMaterializer) materialize(ctx context.Context, p *napCatReceiveParsedMessage, depth int) {
	if p == nil {
		return
	}
	if len(p.Segments) > 0 {
		for _, seg := range p.Segments {
			switch seg.Type {
			case "node", "forward", "record", "video", "file":
				if ctx.Err() != nil {
					m.appendText("\n[附件或转发接收已中断]\n")
				} else if seg.Type == "node" || seg.Type == "forward" {
					m.forward(ctx, seg, depth)
				} else {
					m.media(ctx, seg)
				}
			default:
				part := parseNapCatReceiveSegments([]napCatIncomingSegment{seg}, false)
				if part == nil {
					continue
				}
				m.appendText(part.Text)
				for _, image := range part.ImageSegments {
					if ctx.Err() != nil {
						m.appendText("\n[图片接收已中断]\n")
						continue
					}
					var item napCatIncomingSegment
					item.Type, item.Data.napCatMessageSegmentData = "image", image.Data
					m.media(ctx, item)
				}
			}
		}
		return
	}
	// Compatibility for in-memory callers that construct parsed input directly.
	m.appendText(p.Text)
	for _, image := range p.ImageSegments {
		var seg napCatIncomingSegment
		seg.Type = "image"
		seg.Data.napCatMessageSegmentData = image.Data
		m.media(ctx, seg)
	}
	for _, seg := range p.MediaSegments {
		if ctx.Err() != nil {
			m.appendText("\n[附件或转发接收已中断]")
			return
		}
		switch seg.Type {
		case "node", "forward":
			m.forward(ctx, seg, depth)
		default:
			m.media(ctx, seg)
		}
	}
}

func (m *napCatReceiveMaterializer) forward(ctx context.Context, seg napCatIncomingSegment, depth int) {
	if depth >= napCatReceiveMaxDepth || m.budget.nodes >= napCatReceiveMaxNodes {
		m.appendText("\n[合并转发超出展开限额]")
		return
	}
	m.budget.nodes++
	raw := seg.Data.Content
	if len(raw) == 0 {
		raw = seg.Data.Message
	}
	if len(raw) == 0 {
		id := seg.Data.idString()
		if id == "" {
			m.appendText("\n[合并转发内容不可用]")
			return
		}
		if m.budget.seen == nil {
			m.budget.seen = map[string]bool{}
		}
		key := seg.Type + ":" + id
		if m.budget.seen[key] {
			m.appendText("\n[重复或循环转发已省略]")
			return
		}
		m.budget.seen[key] = true
		action, params := "get_forward_msg", map[string]interface{}{"message_id": id}
		if seg.Type == "node" {
			action = "get_msg"
		}
		response, err := m.action(ctx, action, params)
		if err != nil || len(response) > napCatReceiveMaxRaw {
			m.appendText("\n[合并转发获取失败]")
			return
		}
		var envelope struct {
			Messages json.RawMessage `json:"messages"`
			Message  json.RawMessage `json:"message"`
		}
		if json.Unmarshal(response, &envelope) != nil {
			m.appendText("\n[合并转发解析失败]")
			return
		}
		raw = envelope.Messages
		if seg.Type == "node" {
			raw = envelope.Message
		}
	}
	if len(raw) > napCatReceiveMaxRaw {
		m.appendText("\n[合并转发超出展开限额]")
		return
	}
	m.appendText("\n[合并转发开始]\n")
	// data.messages contains sender/message (some implementations use content).
	var nodes []struct {
		Type   string `json:"type"`
		Sender struct {
			Nickname string `json:"nickname"`
			Card     string `json:"card"`
		} `json:"sender"`
		Message json.RawMessage `json:"message"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &nodes) == nil && len(nodes) > 0 && nodes[0].Type == "" {
		for _, node := range nodes {
			if m.budget.nodes >= napCatReceiveMaxNodes {
				m.appendText("\n[合并转发超出节点限额]")
				break
			}
			m.budget.nodes++
			name := node.Sender.Card
			if name == "" {
				name = node.Sender.Nickname
			}
			if name != "" {
				m.appendText("\n" + napCatReceiveName(name, "转发者") + ": ")
			}
			content := node.Message
			if len(content) == 0 {
				content = node.Content
			}
			m.expand(ctx, content, depth+1)
		}
	} else {
		name := seg.Data.Name
		if name == "" {
			name = seg.Data.Nickname
		}
		if name != "" {
			m.appendText(napCatReceiveName(name, "转发者") + ": ")
		}
		m.expand(ctx, raw, depth+1)
	}
	m.appendText("\n[合并转发结束]\n")
}
func (m *napCatReceiveMaterializer) expand(ctx context.Context, raw json.RawMessage, depth int) {
	if len(raw) == 0 {
		m.appendText("[转发内容不可用]")
		return
	}
	p, err := parseNapCatReceiveMessage(raw, "", false)
	if err != nil {
		m.appendText("[转发内容解析失败]")
		return
	}
	m.materialize(ctx, p, depth)
}

func napCatReceiveName(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ':' {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = fallback
	}
	if len(name) > 200 {
		name = name[:200]
		for !utf8.ValidString(name) {
			name = name[:len(name)-1]
		}
	}
	return name
}
func napCatReceiveHTTP(ref string) bool {
	u, e := url.Parse(strings.TrimSpace(ref))
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (m *napCatReceiveMaterializer) media(ctx context.Context, seg napCatIncomingSegment) {
	label := map[string]string{"image": "图片", "record": "语音", "video": "视频", "file": "文件"}[seg.Type]
	if label == "" {
		label = "未知类型"
	}
	if m.budget.media >= napCatReceiveMaxMedia || m.budget.bytes >= storage.MaxAttachmentBytes {
		m.appendText("\n[附件超出接收限额]")
		return
	}
	m.budget.media++
	limit := int64(storage.MaxAttachmentBytes) - m.budget.bytes
	if seg.Type == "image" && limit > maxChatImageBodyBytes {
		limit = maxChatImageBodyBytes
	}
	data, converted, err := m.readMedia(ctx, seg, limit)
	if err != nil {
		m.appendText("\n[" + label + "附件接收失败，内容不可用]")
		return
	}
	m.budget.bytes += int64(len(data))
	if seg.Type == "image" {
		path, err := m.saveImage(data)
		if err != nil {
			m.appendText("\n[图片保存失败]")
			return
		}
		m.images = append(m.images, path)
		m.appendText("\n[用户发送了图片]\n")
		return
	}
	kind := seg.Type
	fallback := "附件.bin"
	if kind == "record" {
		kind = "audio"
		fallback = "语音.silk"
		if converted {
			fallback = "语音.mp3"
		}
	}
	if kind == "video" {
		fallback = "视频.mp4"
	}
	name := seg.Data.FileName
	if name == "" {
		name = seg.Data.Name
	}
	// NapCat file segments commonly use `file` for the display filename and
	// `file_id` for the opaque resource ID.
	if name == "" && seg.Type == "file" && seg.Data.FileID != "" && seg.Data.File != seg.Data.FileID {
		name = seg.Data.File
	}
	name = napCatReceiveName(name, fallback)
	if converted {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + ".mp3"
	}
	a, err := m.saveAttachment(data, name, kind)
	if err != nil {
		m.appendText("\n[附件保存失败，内容不可用]")
		return
	}
	m.attachments = append(m.attachments, a)
	m.appendText(fmt.Sprintf("\n[收到%s附件：%s；已保存，仅作为附件提供，未转写或读取其内容]", label, name))
}

func (m *napCatReceiveMaterializer) readMedia(ctx context.Context, seg napCatIncomingSegment, limit int64) ([]byte, bool, error) {
	id := strings.TrimSpace(seg.Data.FileID)
	if id == "" {
		id = strings.TrimSpace(seg.Data.File)
	}
	type attempt struct {
		action    string
		params    map[string]interface{}
		converted bool
	}
	validContent := func(data []byte) bool {
		return seg.Type != "image" || strings.HasPrefix(http.DetectContentType(data), "image/")
	}
	// Prefer an existing download URL, except for voice where get_record can
	// produce a playable MP3. Avoid making every image wait for an extra API.
	if seg.Type != "record" {
		for _, ref := range []string{seg.Data.URL, seg.Data.File} {
			if napCatReceiveHTTP(ref) {
				if data, err := readNapCatResource(ctx, ref, limit); err == nil && validContent(data) {
					return data, false, nil
				}
			}
		}
	}
	var attempts []attempt
	if id != "" && !looksLikeNapCatLocalFilePath(id) && !strings.Contains(id, ":") {
		switch seg.Type {
		case "image":
			attempts = append(attempts, attempt{"get_image", map[string]interface{}{"file": id}, false})
		case "file":
			attempts = append(attempts, attempt{"get_private_file_url", map[string]interface{}{"file_id": id}, false})
		case "record":
			attempts = append(attempts, attempt{"get_record", map[string]interface{}{"file": id, "out_format": "mp3"}, true})
		}
		attempts = append(attempts, attempt{"get_file", map[string]interface{}{"file_id": id}, false})
	}
	for _, a := range attempts {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		raw, err := m.action(ctx, a.action, a.params)
		if err != nil {
			continue
		}
		// Bound base64 before decoding. Local paths are accepted ONLY from API responses.
		if int64(len(raw)) > (limit+2)/3*4+(64<<10) {
			continue
		}
		var resource napCatGetImageData
		if json.Unmarshal(raw, &resource) != nil {
			continue
		}
		data, err := readNapCatResolvedResource(ctx, &resource, limit, validContent)
		if err == nil {
			return data, a.converted, nil
		}
	}
	if seg.Type == "record" {
		for _, ref := range []string{seg.Data.URL, seg.Data.File} {
			if napCatReceiveHTTP(ref) {
				data, err := readNapCatResource(ctx, ref, limit)
				if err == nil {
					return data, false, nil
				}
			}
		}
	}
	return nil, false, errors.New("NapCat media unavailable")
}
