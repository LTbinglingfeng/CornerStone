package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"encoding/json"
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
