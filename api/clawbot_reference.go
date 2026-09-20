package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
	"path"
	"strings"
)

func (s *ClawBotService) cacheClawBotReference(cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage) {
	if s.stateManager == nil || msg.MessageID == "" || strings.TrimSpace(msg.FromUserID) == "" {
		return
	}
	raw, err := json.Marshal(msg.ItemList)
	if err != nil {
		return
	}
	_ = s.stateManager.SaveClawBotReference(clawBotAccount(cfg), storage.ClawBotReferenceConversation(strings.TrimSpace(msg.FromUserID), msg.SessionID), string(msg.MessageID), raw)
}

// restoreClawBotReferences must run before attachment preparation. Cache raw
// items first, never the synthesized text/attachments. Cache IO is non-fatal.
func (s *ClawBotService) restoreClawBotReferences(cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage) client.ClawBotIncomingMessage {
	s.cacheClawBotReference(cfg, msg)
	return client.RestoreClawBotReferences(msg, func(id client.ClawBotMessageID) json.RawMessage {
		if s.stateManager == nil {
			return nil
		}
		raw, _ := s.stateManager.FindClawBotReference(clawBotAccount(cfg), storage.ClawBotReferenceConversation(strings.TrimSpace(msg.FromUserID), msg.SessionID), string(id))
		return raw
	})
}

// recordClawBotSentReference is the send queue hook, called only after a
// successful send response supplies a server message ID. protocolSessionID is
// the incoming Weixin session_id, NOT the CornerStone job/session identifier.
// For proactive sends without such a session pass empty; lookup never crosses
// session boundaries. Retries without a confirmed server ID must not call it.
func (s *ClawBotService) recordClawBotSentReference(cfg config.ClawBotConfig, userID, protocolSessionID string, id client.ClawBotMessageID, text string) {
	s.cacheClawBotReference(cfg, client.ClawBotIncomingMessage{MessageID: id, FromUserID: userID, SessionID: protocolSessionID, ItemList: []client.ClawBotIncomingMessageItem{{Type: 1, TextItem: &client.ClawBotItemText{Text: text}}}})
}

// prepareClawBotReferences returns trusted local metadata separately from wire items.
func (s *ClawBotService) prepareClawBotReferences(cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage) (client.ClawBotIncomingMessage, []storage.ChatAttachment) {
	s.cacheClawBotReference(cfg, msg)
	var attachments []storage.ChatAttachment
	conversation := storage.ClawBotReferenceConversation(strings.TrimSpace(msg.FromUserID), msg.SessionID)
	account := clawBotAccount(cfg)
	local := map[client.ClawBotMessageID][]storage.ClawBotReferenceMedia{}
	resolved := client.RestoreClawBotReferencesWithMedia(msg, func(id client.ClawBotMessageID) json.RawMessage {
		if s.stateManager == nil {
			return nil
		}
		raw, _ := s.stateManager.FindClawBotReference(account, conversation, string(id))
		local[id], _ = s.stateManager.FindClawBotReferenceMedia(account, conversation, string(id))
		return raw
	}, func(id client.ClawBotMessageID, index int) bool {
		for _, m := range local[id] {
			if m.ItemIndex != index {
				continue
			}
			a := m.Attachment
			if a.Name == "" {
				a.Name = path.Base(a.Path)
			}
			if _, err := s.handler.readClawBotOutgoingMedia(&storage.ClawBotDeliveryMedia{SourcePath: a.Path, Name: a.Name, Kind: a.Kind}); err != nil {
				return false
			}
			attachments = append(attachments, a)
			return true
		}
		return false
	})
	return resolved, attachments
}

func (s *ClawBotService) saveClawBotLocalReference(cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage, media []storage.ClawBotReferenceMedia) {
	if s.stateManager == nil || msg.MessageID == "" || len(media) == 0 {
		return
	}
	_ = s.stateManager.SaveClawBotReferenceMedia(clawBotAccount(cfg), storage.ClawBotReferenceConversation(strings.TrimSpace(msg.FromUserID), msg.SessionID), string(msg.MessageID), media)
}

// recordClawBotSentMediaReference must follow caching the confirmed full wire item.
// SourcePath/Name/Kind come from the trusted delivery chunk, not inbound JSON.
func (s *ClawBotService) recordClawBotSentMediaReference(cfg config.ClawBotConfig, userID, protocolSessionID string, id client.ClawBotMessageID, sourcePath, name, kind string) {
	if name == "" {
		name = path.Base(sourcePath)
	}
	data, err := s.handler.readClawBotOutgoingMedia(&storage.ClawBotDeliveryMedia{SourcePath: sourcePath, Name: name, Kind: kind})
	if err != nil {
		return
	}
	a := storage.ChatAttachment{Path: sourcePath, Name: name, Kind: kind, Size: int64(len(data))}
	s.saveClawBotLocalReference(cfg, client.ClawBotIncomingMessage{MessageID: id, FromUserID: userID, SessionID: protocolSessionID}, []storage.ClawBotReferenceMedia{{ItemIndex: 0, Attachment: a}})
}
