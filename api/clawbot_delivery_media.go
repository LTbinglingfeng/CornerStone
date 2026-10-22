package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

func deliveryMediaNameOK(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 240 && !strings.ContainsAny(name, "/\\:") && strings.IndexFunc(name, unicode.IsControl) < 0
}

// Build a stable ordered list without performing network or filesystem work.
// Every item receives its own fixed correlation ID and acknowledgement cursor.
func buildClawBotDeliveryChunks(id, text, token string, messages []storage.ChatMessage) []storage.ClawBotDeliveryChunk {
	for _, message := range messages {
		if message.Role == "tool" {
			var result chatToolResult
			if json.Unmarshal([]byte(message.Content), &result) == nil && result.OK && result.Tool == "no_reply" {
				return nil
			}
		}
	}
	var chunks []storage.ClawBotDeliveryChunk
	final := -1
	for i, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" {
			final = i
		}
	}
	addText := func(index int) {
		for _, part := range splitClawBotReplyMessages(text, token, clawBotReplyChunkMaxRune) {
			idx := index
			chunks = append(chunks, storage.ClawBotDeliveryChunk{Text: part, Status: "pending", MessageIndex: &idx})
		}
	}
	addMedia := func(a storage.ChatAttachment, index int) {
		kind := a.Kind
		if kind == "audio" {
			kind = "file"
		} // No proven outbound voice protocol: send the original as a file.
		name := a.Name
		if name == "" {
			name = path.Base(a.Path)
		}
		idx := index
		chunks = append(chunks, storage.ClawBotDeliveryChunk{Status: "pending", MessageIndex: &idx, Media: &storage.ClawBotDeliveryMedia{Kind: kind, Name: name, MIMEType: a.MIMEType, Size: a.Size, SourcePath: a.Path}})
	}
	seenCalls := map[string]bool{}
	for i, m := range messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			if seenCalls[m.ToolCallID] {
				continue
			}
			seenCalls[m.ToolCallID] = true
		}
		if i == final {
			addText(i)
		}
		if m.Role != "assistant" && m.Role != "tool" {
			continue
		}
		seen := map[string]bool{}
		add := func(a storage.ChatAttachment) {
			if !seen[a.Path] {
				addMedia(a, i)
				seen[a.Path] = true
			}
		}
		for _, p := range m.ImagePaths {
			add(storage.ChatAttachment{Path: p, Kind: "image"})
		}
		for _, p := range m.TTSAudioPaths {
			add(storage.ChatAttachment{Path: p, Kind: "audio"})
		}
		for _, a := range m.Attachments {
			add(a)
		}
		if m.Role == "tool" {
			if a, ok := attachmentFromToolResult(m.Content); ok {
				add(a)
			}
		}
	}
	if final < 0 {
		addText(len(messages))
	}
	for i := range chunks {
		chunks[i].ClientID = fmt.Sprintf("cs-%s-%d", id, i)
	}
	return chunks
}

func (h *Handler) readClawBotOutgoingMedia(media *storage.ClawBotDeliveryMedia) ([]byte, error) {
	if media == nil || !deliveryMediaNameOK(media.Name) {
		return nil, errors.New("invalid outgoing filename")
	}
	if strings.HasPrefix(media.SourcePath, storage.AttachmentURLPrefix) {
		return h.attachmentStore().Read(media.SourcePath)
	}
	var dir, prefix string
	switch {
	case strings.HasPrefix(media.SourcePath, "cache_photo/"):
		dir = h.cachePhotoDir
		prefix = "cache_photo/"
	case strings.HasPrefix(media.SourcePath, "tts_audio/"):
		dir = h.ttsAudioDir
		prefix = "tts_audio/"
	default:
		return nil, errors.New("unregistered outgoing media path")
	}
	name := strings.TrimPrefix(media.SourcePath, prefix)
	if name != filepath.Base(name) || strings.ContainsAny(name, "/\\") || storage.ValidateFileName(name) != nil {
		return nil, errors.New("invalid outgoing media path")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > storage.ClawBotDeliveryMediaMaxBytes {
		return nil, errors.New("invalid outgoing media file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, storage.ClawBotDeliveryMediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > storage.ClawBotDeliveryMediaMaxBytes {
		return nil, errors.New("outgoing media too large")
	}
	return data, nil
}

func outgoingClawBotMediaType(media *storage.ClawBotDeliveryMedia, data []byte) (int, error) {
	if len(data) == 0 || len(data) > storage.ClawBotDeliveryMediaMaxBytes || !deliveryMediaNameOK(media.Name) {
		return 0, errors.New("invalid outgoing media")
	}
	detected := strings.Split(http.DetectContentType(data), ";")[0]
	switch media.Kind {
	case "image":
		if !strings.HasPrefix(detected, "image/") {
			return 0, errors.New("invalid image content")
		}
		return 1, nil
	case "video":
		if !strings.HasPrefix(detected, "video/") {
			return 0, errors.New("invalid video content")
		}
		return 2, nil
	case "file":
		return 3, nil
	default:
		return 0, errors.New("unsupported outgoing media kind")
	}
}

// uploadDeliveryChunk is safely restartable: it never calls sendmessage. A
// durable upload receipt moves the job back to ready for a separate send step.
func (s *ClawBotService) uploadDeliveryChunk(ctx context.Context, cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob) {
	index := job.NextChunk
	if err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		if j.Status != "ready" || j.Revision != job.Revision || j.NextChunk != index {
			return errClawBotJobUnavailable
		}
		j.Status = "uploading"
		j.Phase = "upload"
		j.Chunks[index].Status = "uploading"
		return nil
	}); err != nil {
		if !errors.Is(err, errClawBotJobUnavailable) {
			s.deliveryFault(err)
		}
		return
	}
	media := job.Chunks[index].Media
	fail := func() {
		err := s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
			if j.Status != "uploading" || j.NextChunk != index || j.CancelRequested {
				return errClawBotJobUnavailable
			}
			j.Chunks[index].Status = "pending"
			// No message was sent for this item; drop the rest instead of waiting
			// for a manual retry. Sent segments are still committed to history.
			j.CancelRequested = true
			j.Status = "committing"
			j.Phase = "history"
			j.LastError = "Attachment preparation or upload failed; the remaining segments were cancelled."
			logging.Warnf("clawbot attachment upload failed, delivery cancelled: job=%s chunk=%d", j.ID, index)
			return nil
		})
		if err != nil && !errors.Is(err, errClawBotJobUnavailable) {
			s.deliveryFault(err)
		}
	}
	var data []byte
	var err error
	if media.BlobID != "" {
		data, err = s.stateManager.ReadDeliveryMedia(clawBotAccount(cfg), media.BlobID)
	} else {
		data, err = s.handler.readClawBotOutgoingMedia(media)
		if err == nil {
			_, err = outgoingClawBotMediaType(media, data)
		}
		if err == nil {
			_ = s.stateManager.CleanupDeliveryMedia(clawBotAccount(cfg), time.Now())
			media.BlobID, err = s.stateManager.SaveDeliveryMedia(clawBotAccount(cfg), data)
		}
		if err == nil {
			err = s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
				if j.Status != "uploading" || j.NextChunk != index || j.CancelRequested {
					return errClawBotJobUnavailable
				}
				j.Chunks[index].Media.BlobID = media.BlobID
				j.Chunks[index].Media.Size = int64(len(data))
				return nil
			})
			if err != nil {
				if !errors.Is(err, errClawBotJobUnavailable) {
					s.deliveryFault(err)
				}
				return
			}
		}
	}
	if err != nil {
		fail()
		return
	}
	kind, err := outgoingClawBotMediaType(media, data)
	if err != nil {
		fail()
		return
	}
	// Keep a separate browser/history copy of the immutable upload bytes. Tool
	// source files can disappear after generation without making sent history a
	// dangling reference. Neither copy is a message acknowledgement.
	if media.HistoryPath == "" {
		if media.Kind == "image" {
			media.HistoryPath, err = s.handler.saveCachePhotoBytes(data)
		} else {
			var stored storage.ChatAttachment
			stored, err = s.handler.attachmentStore().Save(data, media.Name, media.Kind)
			media.HistoryPath = stored.Path
			media.MIMEType = stored.MIMEType
		}
		if err != nil {
			fail()
			return
		}
		if err = s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
			if j.Status != "uploading" || j.NextChunk != index || j.CancelRequested {
				return errClawBotJobUnavailable
			}
			j.Chunks[index].Media.HistoryPath = media.HistoryPath
			j.Chunks[index].Media.MIMEType = media.MIMEType
			return nil
		}); err != nil {
			if !errors.Is(err, errClawBotJobUnavailable) {
				s.deliveryFault(err)
			}
			return
		}
	}
	receipt, err := s.client.UploadMedia(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, job.UserID, kind, data)
	if err != nil {
		fail()
		if client.IsClawBotStaleToken(err) {
			s.markTokenStale(cfg.BotToken)
		}
		return
	}
	if err = s.changeDelivery(cfg, job.ID, func(j *storage.ClawBotDeliveryJob) error {
		if j.Status != "uploading" || j.NextChunk != index {
			return errClawBotJobUnavailable
		}
		j.Chunks[index].Media.Uploaded = receipt
		j.Chunks[index].Media.Size = int64(len(data))
		j.Chunks[index].Status = "pending"
		j.Status = "ready"
		j.Phase = "send"
		j.LastError = ""
		return nil
	}); err != nil && !errors.Is(err, errClawBotJobUnavailable) {
		s.deliveryFault(err)
	}
}

func clawBotDeliveryWireItem(chunk storage.ClawBotDeliveryChunk) client.ClawBotIncomingMessageItem {
	if chunk.Media == nil {
		return client.ClawBotIncomingMessageItem{Type: 1, TextItem: &client.ClawBotItemText{Text: chunk.Text}}
	}
	m := chunk.Media
	u := m.Uploaded
	if u == nil {
		return client.ClawBotIncomingMessageItem{}
	}
	switch m.Kind {
	case "image":
		return client.ClawBotIncomingMessageItem{Type: 2, ImageItem: &client.ClawBotImageItem{Media: &u.Media, MidSize: u.CipherSize}}
	case "video":
		return client.ClawBotIncomingMessageItem{Type: 5, VideoItem: &client.ClawBotVideoItem{Media: &u.Media, VideoSize: u.CipherSize}}
	default:
		return client.ClawBotIncomingMessageItem{Type: 4, FileItem: &client.ClawBotFileItem{Media: &u.Media, FileName: m.Name, Len: fmt.Sprint(u.RawSize)}}
	}
}
