package api

import (
	"context"
	"cornerstone/storage"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode"
)

// readNapCatOutgoingAttachment never fetches URLs or accepts caller filesystem paths.
// OpenRoot also confines access if a file is replaced between validation and open.
func (h *Handler) readNapCatOutgoingAttachment(a storage.ChatAttachment) ([]byte, error) {
	if !validToolAttachmentPath(a.Path) {
		return nil, errors.New("unregistered outgoing attachment path")
	}
	if strings.HasPrefix(a.Path, storage.AttachmentURLPrefix) {
		return h.attachmentStore().Read(a.Path)
	}
	var dir, prefix string
	switch {
	case strings.HasPrefix(a.Path, "cache_photo/"):
		dir, prefix = h.cachePhotoDir, "cache_photo/"
	case strings.HasPrefix(a.Path, "tts_audio/"):
		dir, prefix = h.ttsAudioDir, "tts_audio/"
	default:
		return nil, errors.New("unregistered outgoing attachment path")
	}
	name := strings.TrimPrefix(a.Path, prefix)
	if storage.ValidateFileName(name) != nil {
		return nil, errors.New("invalid attachment filename")
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
	if !info.Mode().IsRegular() || info.Size() > storage.MaxAttachmentBytes {
		return nil, errors.New("invalid outgoing attachment file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > storage.MaxAttachmentBytes {
		return nil, errors.New("invalid outgoing attachment file")
	}
	data, err := io.ReadAll(io.LimitReader(f, storage.MaxAttachmentBytes+1))
	if len(data) > storage.MaxAttachmentBytes {
		return nil, errors.New("outgoing attachment exceeds limit")
	}
	return data, err
}

// NapCat's MsgApi.handleOb11FileLikeMessage uses uriToLocalFile for image,
// record, video and file, which supports base64://. upload_private_file uses the
// same resolver and preserves the supplied name. No shared filesystem is needed.
func (s *NapCatService) sendPrivateAttachment(ctx context.Context, userID int64, a storage.ChatAttachment) error {
	if s == nil || s.handler == nil || userID <= 0 {
		return errors.New("invalid private attachment destination")
	}
	kind := a.Kind
	switch kind {
	case "audio":
		kind = "record"
	case "image", "video", "file":
	default:
		return errors.New("unsupported attachment kind")
	}
	name := a.Name
	if name == "" {
		name = path.Base(a.Path)
	}
	if name == "." || name == ".." || len(name) > 240 || strings.ContainsAny(name, "/\\:") || strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return errors.New("invalid attachment name")
	}
	data, err := s.handler.readNapCatOutgoingAttachment(a)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("empty outgoing attachment")
	}
	file := "base64://" + base64.StdEncoding.EncodeToString(data)
	if kind == "file" {
		return s.sendConfirmedAction(ctx, "upload_private_file", map[string]interface{}{"user_id": fmt.Sprint(userID), "file": file, "name": name, "upload_file": true})
	}
	return s.sendConfirmedAction(ctx, "send_private_msg", map[string]interface{}{"user_id": userID, "message": []map[string]interface{}{{"type": kind, "data": map[string]string{"file": file, "name": name}}}})
}

func napCatAttachmentIntent(m storage.ChatMessage) (storage.ChatAttachment, bool) {
	if m.Role != "tool" || m.ToolCallID == "" {
		return storage.ChatAttachment{}, false
	}
	var result chatToolResult
	if json.Unmarshal([]byte(m.Content), &result) != nil || result.Tool != sendAttachmentToolName {
		return storage.ChatAttachment{}, false
	}
	return attachmentFromToolResult(m.Content)
}

func napCatReplyAttachments(messages []storage.ChatMessage) []storage.ChatAttachment {
	var out []storage.ChatAttachment
	seen := map[string]bool{}
	calls := map[string]bool{}
	add := func(a storage.ChatAttachment) {
		if !seen[a.Path] {
			seen[a.Path] = true
			out = append(out, a)
		}
	}
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, p := range m.ImagePaths {
				add(storage.ChatAttachment{Path: p, Kind: "image"})
			}
			for _, p := range m.TTSAudioPaths {
				add(storage.ChatAttachment{Path: p, Kind: "audio"})
			}
			for _, a := range m.Attachments {
				add(a)
			}
		}
		if a, ok := napCatAttachmentIntent(m); ok && !calls[m.ToolCallID] {
			calls[m.ToolCallID] = true
			add(a)
		}
	}
	return out
}

// sendGeneratedPrivateReply performs each action once, waits for its echo, then
// records only confirmed content. A timeout is ambiguous, not permission to retry.
// On partial failure remaining sends stop; tool history explicitly records that
// delivery is unconfirmed and removes its attachment metadata from future access.
// The caller must not retry this helper automatically (including persistence errors).
func (s *NapCatService) sendGeneratedPrivateReply(ctx context.Context, source napCatChatSource, sessionID string, reply *napCatGeneratedReply) error {
	if reply == nil || !source.isPrivate() {
		return nil
	}
	if s == nil || s.handler == nil || s.handler.chatManager == nil {
		return errors.New("napcat chat storage unavailable")
	}
	silent := false
	for _, m := range reply.StorageMessages {
		var r chatToolResult
		if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &r) == nil && r.OK && r.Tool == "no_reply" {
			silent = true
		}
	}
	sent := map[string]bool{}
	var deliveryErr error
	if !silent {
		for _, a := range napCatReplyAttachments(reply.StorageMessages) {
			if err := s.sendPrivateAttachment(ctx, source.UserID, a); err != nil {
				deliveryErr = fmt.Errorf("attachment delivery unconfirmed; do not automatically retry: %w", err)
				break
			}
			sent[a.Path] = true
		}
	}
	token := configuredAssistantMessageSplitToken(s.handler.configManager)
	var confirmedText []string
	if !silent && deliveryErr == nil {
		for _, text := range splitAssistantReplyMessages(strings.TrimSpace(reply.Text), token, napCatReplyChunkMaxRune) {
			// A structured text segment prevents CQ-code interpretation of model prose.
			if err := s.sendConfirmedAction(ctx, "send_private_msg", map[string]interface{}{"user_id": source.UserID, "message": []map[string]interface{}{{"type": "text", "data": map[string]string{"text": text}}}}); err != nil {
				deliveryErr = fmt.Errorf("text delivery unconfirmed; do not automatically retry: %w", err)
				break
			}
			confirmedText = append(confirmedText, text)
		}
	}
	messages := make([]storage.ChatMessage, 0, len(reply.StorageMessages)+1)
	for _, original := range reply.StorageMessages {
		m := original
		if m.Role == "assistant" || m.Role == "tool" {
			m.ImagePaths, m.TTSAudioPaths, m.Attachments = nil, nil, nil
			if m.Role == "assistant" {
				m.Content = ""
				for _, p := range original.ImagePaths {
					if sent[p] {
						m.ImagePaths = append(m.ImagePaths, p)
					}
				}
				for _, p := range original.TTSAudioPaths {
					if sent[p] {
						m.TTSAudioPaths = append(m.TTSAudioPaths, p)
					}
				}
				for _, a := range original.Attachments {
					if sent[a.Path] {
						m.Attachments = append(m.Attachments, a)
					}
				}
			}
		}
		if a, ok := napCatAttachmentIntent(original); ok {
			if sent[a.Path] {
				m.Content = marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, OK: true, Data: map[string]interface{}{"attachment": a, "delivery_status": "confirmed"}})
			} else {
				m.Content = marshalChatToolResult(chatToolResult{Tool: sendAttachmentToolName, Error: "Attachment delivery was not confirmed or was not attempted. Do not claim it was sent or automatically retry."})
			}
		}
		// Preserve reasoning and tool-call pairs even when no visible output arrived.
		if m.Role == "assistant" && m.Content == "" && m.ReasoningContent == "" && len(m.ToolCalls)+len(m.ImagePaths)+len(m.TTSAudioPaths)+len(m.Attachments) == 0 {
			continue
		}
		messages = append(messages, m)
	}
	// Tool-intent media also needs visible assistant metadata, not only a JSON
	// tool receipt, so history/UI and subsequent session attachment catalogs agree.
	delivered := storage.ChatMessage{Role: "assistant", Timestamp: time.Now()}
	represented := make(map[string]bool)
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, p := range append(append([]string(nil), m.ImagePaths...), m.TTSAudioPaths...) {
			represented[p] = true
		}
		for _, a := range m.Attachments {
			represented[a.Path] = true
		}
	}
	for _, a := range napCatReplyAttachments(reply.StorageMessages) {
		if !sent[a.Path] || represented[a.Path] {
			continue
		}
		if a.Kind == "image" {
			delivered.ImagePaths = append(delivered.ImagePaths, a.Path)
		} else if a.Kind == "audio" && strings.HasPrefix(a.Path, "tts_audio/") {
			delivered.TTSAudioPaths = append(delivered.TTSAudioPaths, a.Path)
		} else {
			delivered.Attachments = append(delivered.Attachments, a)
		}
	}
	if len(confirmedText) > 0 {
		separator := token
		if separator == "" {
			separator = "\n"
		}
		delivered.Content = strings.Join(confirmedText, separator)
	}
	if delivered.Content != "" || len(delivered.ImagePaths)+len(delivered.TTSAudioPaths)+len(delivered.Attachments) > 0 {
		messages = append(messages, delivered)
	}
	if deliveryErr != nil {
		// A neutral delivery note preserves partial/ambiguous failure even when the
		// failed media came directly from assistant metadata rather than a tool.
		messages = append(messages, storage.ChatMessage{Role: "system", Content: "[NapCat delivery] Some reply content was not confirmed delivered; remaining sends were stopped. Do not claim unsent attachments were delivered or automatically retry.", Timestamp: time.Now()})
	}
	ensureAssistantMessageSplitToken(messages, token)
	if len(messages) > 0 {
		if err := s.handler.chatManager.AddMessages(sessionID, messages); err != nil {
			deliveryErr = errors.Join(deliveryErr, fmt.Errorf("save delivered reply (do not resend): %w", err))
		}
	}
	if deliveryErr != nil {
		s.setLastError(deliveryErr)
		return deliveryErr
	}
	if reply.MemSession != nil {
		reply.MemSession.OnRoundComplete()
	}
	return nil
}
