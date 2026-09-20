package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const clawBotRecentMessageLimit = 2048

func clawBotAccount(cfg config.ClawBotConfig) string {
	return storage.ClawBotAccountKey(cfg.BaseURL, strings.TrimSpace(cfg.BotToken))
}

func (s *ClawBotService) isCurrentAccount(cfg config.ClawBotConfig) bool {
	return clawBotAccount(cfg) == clawBotAccount(s.handler.configManager.GetClawBotConfig())
}

func (s *ClawBotService) restoreChannelState(cfg config.ClawBotConfig) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	account := clawBotAccount(cfg)
	if s.stateAccount == account {
		return nil
	}
	state := storage.ClawBotState{}
	if s.stateManager != nil && strings.TrimSpace(cfg.BotToken) != "" {
		var err error
		state, err = s.stateManager.Load(account)
		if err != nil {
			return err
		}
	}
	if state.ContextTokens == nil {
		state.ContextTokens = make(map[string]string)
	}
	if state.GetUpdatesBuf == "" {
		state.GetUpdatesBuf = strings.TrimSpace(cfg.GetUpdatesBuf)
	}
	s.clearAllPendingReplies()
	s.mu.Lock()
	s.contextTokens = maps.Clone(state.ContextTokens)
	s.activeSessions = make(map[string]*clawBotActiveSession)
	s.mu.Unlock()
	s.stateAccount, s.state = account, state
	return nil
}

func (s *ClawBotService) pollCursor(cfg config.ClawBotConfig) string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.stateAccount == clawBotAccount(cfg) {
		return s.state.GetUpdatesBuf
	}
	return strings.TrimSpace(cfg.GetUpdatesBuf)
}

func (s *ClawBotService) updateChannelState(cfg config.ClawBotConfig, update func(*storage.ClawBotState)) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	account := clawBotAccount(cfg)
	if !s.isCurrentAccount(cfg) || (s.stateAccount != "" && s.stateAccount != account) {
		return fmt.Errorf("clawbot account changed; refusing stale state update")
	}
	// Copy before changing: a failed disk write must not advance the in-memory
	// cursor or suppress a subsequently replayed message.
	next := s.state
	next.ContextTokens = maps.Clone(s.state.ContextTokens)
	if next.ContextTokens == nil {
		next.ContextTokens = make(map[string]string)
	}
	next.RecentMessageIDs = slices.Clone(s.state.RecentMessageIDs)
	update(&next)
	if s.stateManager != nil {
		if err := s.stateManager.Save(account, next); err != nil {
			return err
		}
	}
	s.stateAccount, s.state = account, next
	s.mu.Lock()
	for user, token := range next.ContextTokens {
		s.contextTokens[user] = token
	}
	s.mu.Unlock()
	return nil
}

func (s *ClawBotService) saveIncomingContextTokens(cfg config.ClawBotConfig, messages []client.ClawBotIncomingMessage) error {
	tokens := make(map[string]string)
	s.stateMu.Lock()
	for _, msg := range messages {
		user, token := strings.TrimSpace(msg.FromUserID), strings.TrimSpace(msg.ContextToken)
		if msg.MessageType == 1 && user != "" && token != "" && s.state.ContextTokens[user] != token {
			tokens[user] = token
		}
	}
	s.stateMu.Unlock()
	if len(tokens) == 0 {
		return nil
	}
	return s.updateChannelState(cfg, func(state *storage.ClawBotState) {
		maps.Copy(state.ContextTokens, tokens)
	})
}

func clawBotMessageKey(msg client.ClawBotIncomingMessage) string {
	if msg.MessageID == "" || msg.MessageID == "0" {
		return ""
	}
	return strings.TrimSpace(msg.FromUserID) + ":" + string(msg.MessageID)
}

func (s *ClawBotService) alreadyReceived(cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage) bool {
	key := clawBotMessageKey(msg)
	if key == "" {
		return false
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.stateAccount == clawBotAccount(cfg) && slices.Contains(s.state.RecentMessageIDs, key)
}

// Checkpoint only after the batch has been handed to the existing command/reply
// pipeline. This is a receive checkpoint, not an exactly-once delivery receipt:
// generation, outbound sending and chat persistence are separate operations.
func (s *ClawBotService) savePollCheckpoint(cfg config.ClawBotConfig, cursor string, messages []client.ClawBotIncomingMessage) error {
	return s.updateChannelState(cfg, func(state *storage.ClawBotState) {
		state.GetUpdatesBuf = cursor
		for _, msg := range messages {
			key := clawBotMessageKey(msg)
			if key != "" && !slices.Contains(state.RecentMessageIDs, key) {
				state.RecentMessageIDs = append(state.RecentMessageIDs, key)
			}
		}
		if len(state.RecentMessageIDs) > clawBotRecentMessageLimit {
			state.RecentMessageIDs = slices.Clone(state.RecentMessageIDs[len(state.RecentMessageIDs)-clawBotRecentMessageLimit:])
		}
	})
}
