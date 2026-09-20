package api

import (
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/storage"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const clawBotRecentMessageLimit = 2048

func clawBotAccount(cfg config.ClawBotConfig) string {
	if id := strings.TrimSpace(cfg.ILinkBotID); id != "" {
		return storage.ClawBotAccountKey(cfg.BaseURL, "bot-id:"+id)
	}
	return storage.ClawBotAccountKey(cfg.BaseURL, strings.TrimSpace(cfg.BotToken))
}

func (s *ClawBotService) isCurrentAccount(cfg config.ClawBotConfig) bool {
	return clawBotAccount(cfg) == clawBotAccount(s.handler.configManager.GetClawBotConfig())
}

func (s *ClawBotService) restoreChannelState(cfg config.ClawBotConfig) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	account := clawBotAccount(cfg)
	// Persistent authority must be revalidated even for a cached account: a
	// migration may have retired it before the config write failed.
	if s.stateAccount == account && s.stateManager == nil {
		return nil
	}
	state := storage.ClawBotState{}
	if s.stateManager != nil && strings.TrimSpace(cfg.BotToken) != "" {
		var err error
		// Do not migrate or activate a credential whose account was retired.
		// Publish only an audit snapshot so management can still see its queue.
		if _, err := s.stateManager.MigrationTarget(account); err == nil {
			audit, auditErr := s.stateManager.LoadAudit(account)
			if auditErr != nil {
				return auditErr
			}
			s.stateAccount, s.state = account, audit
			return fmt.Errorf("ClawBot account migrated; restore the verified successor credential")
		}
		if cfg.ILinkBotID != "" {
			// Same credential proves the legacy token-key snapshot belongs here.
			if err = s.stateManager.Migrate(storage.ClawBotAccountKey(cfg.BaseURL, strings.TrimSpace(cfg.BotToken)), account); err != nil {
				return err
			}
		}
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
	if state.Delivery != nil {
		for user, sessionID := range state.Delivery.ActiveSessions {
			s.activeSessions[user] = &clawBotActiveSession{SessionID: sessionID, LastActive: time.Now()}
		}
	}
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
	return s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error { update(state); return nil })
}

func (s *ClawBotService) updateChannelStateChecked(cfg config.ClawBotConfig, update func(*storage.ClawBotState) error) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	account := clawBotAccount(cfg)
	if !s.isCurrentAccount(cfg) || (s.stateAccount != "" && s.stateAccount != account) {
		return fmt.Errorf("clawbot account changed; refusing stale state update")
	}
	// Copy before changing: a failed disk write must not advance the in-memory
	// cursor or suppress a subsequently replayed message.
	next, err := storage.CloneClawBotState(s.state)
	if err != nil {
		return err
	}
	if next.ContextTokens == nil {
		next.ContextTokens = make(map[string]string)
	}
	if err := update(&next); err != nil {
		return err
	}
	if s.stateManager != nil {
		if err := s.stateManager.Save(account, next); err != nil {
			return err
		}
	}
	s.stateAccount, s.state = account, next
	s.mu.Lock()
	if s.contextTokens == nil {
		s.contextTokens = make(map[string]string)
	}
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
		if msg.MessageType != 1 || user == "" || token == "" {
			continue
		}
		if key := clawBotMessageKey(msg); key != "" && s.state.ContextTokens[user] != "" && slices.Contains(s.state.RecentMessageIDs, key) {
			continue
		}
		// Pick the last token in the batch before comparing it with saved state.
		tokens[user] = token
	}
	for user, token := range tokens {
		if s.state.ContextTokens[user] == token {
			delete(tokens, user)
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

// replaceClawBotConfigLocked requires credentialMu. Lock order is credentialMu
// -> lifecycleMu -> stateMu. Workers are joined BEFORE migration; an error leaves
// the channel stopped (including uncertain fsync/config-write failures).
func (s *ClawBotService) replaceClawBotConfigLocked(next config.ClawBotConfig) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	old := s.handler.configManager.GetClawBotConfig()
	if clawBotAccount(old) == clawBotAccount(next) && strings.TrimSpace(old.BotToken) == strings.TrimSpace(next.BotToken) {
		// Ordinary persona/permission changes must not interrupt live generation.
		return s.handler.configManager.UpdateClawBotConfig(next)
	}
	s.mu.Lock()
	cancel, done := s.workerCancel, s.workerDone
	s.workerCancel, s.workerDone, s.workerCtx = nil, nil, nil
	s.workerRunning, s.connected = false, false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	if s.stateManager != nil {
		if err := s.restoreChannelState(old); err != nil {
			// A previous identity-verified migration may have committed before
			// config persistence. Only that recorded successor can recover it.
			target, migrationErr := s.stateManager.MigrationTarget(clawBotAccount(old))
			if migrationErr != nil || target != clawBotAccount(next) || strings.TrimSpace(next.BotToken) == "" {
				return err
			}
			return s.handler.configManager.UpdateClawBotConfig(next)
		}
	}
	if err := s.prepareClawBotMigration(old, next); err != nil {
		return err
	}
	if err := s.handler.configManager.UpdateClawBotConfig(next); err != nil {
		return err
	}
	return nil
}

// prepareClawBotMigration uses the OLD configuration as source, never the new
// QR token. Only matching verified bot IDs or identical credentials prove identity.
// Caller has stopped workers and holds credentialMu/lifecycleMu.
func (s *ClawBotService) prepareClawBotMigration(old, next config.ClawBotConfig) error {
	source, target := clawBotAccount(old), clawBotAccount(next)
	if source == target {
		return nil
	}
	sameID := strings.TrimSpace(old.ILinkBotID) != "" && strings.TrimSpace(old.ILinkBotID) == strings.TrimSpace(next.ILinkBotID)
	sameCredential := strings.TrimSpace(old.BotToken) != "" && strings.TrimSpace(old.BotToken) == strings.TrimSpace(next.BotToken) && storage.ClawBotAccountKey(old.BaseURL, "") == storage.ClawBotAccountKey(next.BaseURL, "")
	// Explicitly different verified IDs override token equality.
	if old.ILinkBotID != "" && next.ILinkBotID != "" && old.ILinkBotID != next.ILinkBotID {
		sameCredential = false
	}
	if strings.TrimSpace(next.BotToken) != "" && (sameID || sameCredential) {
		if s.stateManager != nil {
			return s.stateManager.Migrate(source, target)
		}
		return nil
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state.Delivery != nil {
		for _, job := range s.state.Delivery.Jobs {
			if !deliveryTerminal(job.Status) {
				return fmt.Errorf("ClawBot identity cannot be verified; process or cancel pending delivery queue before clearing or switching credentials")
			}
		}
	}
	return nil
}
