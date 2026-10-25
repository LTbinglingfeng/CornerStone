package api

import (
	"context"
	"cornerstone/config"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The legacy deterministic ID remains the first session and is also the scope
// key. New sessions extend it with an unpredictable suffix. Never infer ownership
// from a display title or accept an arbitrary session ID supplied in a command.
func qqOfficialSessionMatches(id, prompt string, j qqOfficialJob) bool {
	scope := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	return prompt == j.cfg.PromptID && (id == scope || strings.HasPrefix(id, scope+"-"))
}

func (s *QQOfficialService) sessionStoreLocked() (*storage.QQOfficialSessionStore, error) {
	if s.sessions == nil {
		root, err := filepath.EvalSymlinks(filepath.Dir(s.handler.cachePhotoDir))
		if err != nil {
			return nil, fmt.Errorf("QQ Official data root unavailable")
		}
		s.sessions = storage.NewQQOfficialSessionStore(filepath.Join(root, "qqofficial", "sessions.json"))
	}
	return s.sessions, nil
}

func (s *QQOfficialService) listSessions(j qqOfficialJob) []storage.ChatSession {
	var sessions []storage.ChatSession
	for _, session := range s.handler.chatManager.ListSessions() {
		if qqOfficialSessionMatches(session.ID, session.PromptID, j) {
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i, k int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[k].UpdatedAt) {
			if sessions[i].CreatedAt.Equal(sessions[k].CreatedAt) {
				return sessions[i].ID < sessions[k].ID
			}
			return sessions[i].CreatedAt.After(sessions[k].CreatedAt)
		}
		return sessions[i].UpdatedAt.After(sessions[k].UpdatedAt)
	})
	return sessions
}

// Caller holds s.mu. Deleted or foreign selections never grant access; fall
// back to the most recent session in this exact app/user/persona scope.
func (s *QQOfficialService) currentSessionLocked(j qqOfficialJob) (*storage.ChatRecord, error) {
	store, err := s.sessionStoreLocked()
	if err != nil {
		return nil, err
	}
	scope := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	id, err := store.Get(scope)
	if err != nil {
		return nil, err
	}
	if record, ok := s.handler.chatManager.GetSession(id); ok && qqOfficialSessionMatches(record.SessionID, record.PromptID, j) {
		return record, nil
	}
	for _, meta := range s.listSessions(j) {
		if record, ok := s.handler.chatManager.GetSession(meta.ID); ok {
			if err := store.Set(scope, record.SessionID); err != nil {
				return nil, err
			}
			return record, nil
		}
	}
	return nil, nil
}

func (s *QQOfficialService) activateSessionLocked(j qqOfficialJob, id string) error {
	store, err := s.sessionStoreLocked()
	if err != nil {
		return err
	}
	return store.Set(qqOfficialSessionID(j.cfg, j.msg.UserOpenID), id)
}

func (s *QQOfficialService) promptName(id string) string {
	if id == "" {
		return "默认"
	}
	if s.handler.promptManager != nil {
		if p, ok := s.handler.promptManager.Get(id); ok {
			return formatClawBotPromptName(*p)
		}
	}
	return id
}

func (s *QQOfficialService) prompts() []storage.Prompt {
	if s.handler.promptManager == nil {
		return nil
	}
	prompts := s.handler.promptManager.List()
	sort.Slice(prompts, func(i, k int) bool {
		left, right := strings.ToLower(formatClawBotPromptName(prompts[i])), strings.ToLower(formatClawBotPromptName(prompts[k]))
		if left == right {
			return prompts[i].ID < prompts[k].ID
		}
		return left < right
	})
	return prompts
}

func qqOfficialCommandConfig(c config.QQOfficialConfig) config.ClawBotConfig {
	return config.ClawBotConfig{CommandPermissions: c.CommandPermissions}
}

func (s *QQOfficialService) commandReply(j qqOfficialJob, reply string) error {
	ctx, cancel := context.WithTimeout(j.ctx, 15*time.Second)
	defer cancel()
	// Use length splitting only: command titles may legitimately contain the
	// assistant's configured separator. Commands never enter model history.
	for i, part := range splitAssistantReplyMessages(reply, "", 2000) {
		s.mu.Lock()
		valid := s.currentLocked(j)
		s.mu.Unlock()
		if !valid {
			return nil
		}
		if _, err := j.conn.SendText(ctx, j.msg.UserOpenID, j.msg.ID, i+1, part); err != nil {
			return err // An uncertain REST POST must never be retried.
		}
	}
	return nil
}

func (s *QQOfficialService) processCommand(j qqOfficialJob, op string, command clawBotCommand, receipts *storage.QQOfficialReceiptStore) error {
	s.mu.Lock()
	if !s.currentLocked(j) {
		s.mu.Unlock()
		return nil
	}
	// Claim before any command side effects or external sends. Independent
	// receipts survive /delete and prevent a replay from repeating /new or /re.
	seen, err := receipts.Seen(op)
	if err == nil && !seen {
		err = receipts.Record(op)
	}
	if err != nil || seen {
		s.mu.Unlock()
		return err
	}
	cfg := qqOfficialCommandConfig(j.cfg)
	if !isKnownClawBotCommand(command.Name) {
		s.mu.Unlock()
		return s.commandReply(j, formatUnknownClawBotCommandReply(cfg, command.Name))
	}
	if !isClawBotCommandAllowed(cfg, command.Name) {
		s.mu.Unlock()
		return s.commandReply(j, formatDisabledClawBotCommandReply(cfg, command.Name))
	}
	if command.Name == clawBotCommandRegenerate && command.Args == "" {
		s.mu.Unlock()
		return s.regenerate(j, op)
	}
	reply, promptChanged, err := s.executeCommandLocked(j, command)
	if promptChanged {
		j.cfg = s.handler.configManager.GetQQOfficialConfig()
	}
	s.mu.Unlock()
	if promptChanged {
		// Send the passive acknowledgement using the old connection before
		// cancelling it; never overwrite a newer settings update on completion.
		defer func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.confirmationCurrentLocked(j) {
				s.applyLocked()
			}
		}()
	}
	if err != nil {
		_ = s.commandReply(j, "命令执行失败，请稍后再试。")
		return err
	}
	return s.commandReply(j, reply)
}

// Mutations and configuration validation are serialized with service lifecycle
// changes. The single worker also serializes commands with ordinary messages.
func (s *QQOfficialService) executeCommandLocked(j qqOfficialJob, command clawBotCommand) (string, bool, error) {
	args := strings.TrimSpace(command.Args)
	cfg := qqOfficialCommandConfig(j.cfg)
	if (command.Name == "menu" || command.Name == "new" || command.Name == "ls" || command.Name == "re") && args != "" {
		return "用法：/" + command.Name, false, nil
	}
	if command.Name == "prompt" {
		return s.promptCommandLocked(j, args)
	}
	current, err := s.currentSessionLocked(j)
	if err != nil {
		return "", false, err
	}
	switch command.Name {
	case "menu":
		title := "未开始"
		if current != nil {
			title = formatClawBotSessionTitle(current.Title)
		}
		lines := []string{"[QQ 官方菜单]", "当前人设：" + s.promptName(j.cfg.PromptID), "当前会话：" + title, "/menu 查看此菜单"}
		for _, entry := range []struct{ name, text string }{
			{"new", "/new 开始新聊天"}, {"ls", "/ls 查看当前人设下的会话列表"},
			{"checkout", "/checkout <序号> 切换会话"}, {"re", "/re 重新生成当前会话的上一轮回复"},
			{"rename", "/rename <标题> 重命名当前会话"}, {"delete", "/delete <序号|current> 删除会话"},
			{"prompt", "/prompt 查看当前人设与用法\n/prompt ls 查看人设列表\n/prompt <序号|id> 切换人设\n注意：/prompt 会切换整个 QQ 官方渠道的人设。"},
		} {
			if isClawBotCommandAllowed(cfg, entry.name) {
				lines = append(lines, entry.text)
			}
		}
		return strings.Join(lines, "\n"), false, nil
	case "new":
		name := ""
		if j.cfg.PromptID != "" {
			if s.handler.promptManager == nil {
				return "", false, fmt.Errorf("configured prompt missing")
			}
			p, ok := s.handler.promptManager.Get(j.cfg.PromptID)
			if !ok {
				return "", false, fmt.Errorf("configured prompt missing")
			}
			name = p.Name
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", false, err
		}
		id := qqOfficialSessionID(j.cfg, j.msg.UserOpenID) + "-" + hex.EncodeToString(random[:])
		if _, err := s.handler.chatManager.CreateSession(id, "New Chat", j.cfg.PromptID, name); err != nil {
			return "", false, err
		}
		if err := s.activateSessionLocked(j, id); err != nil {
			return "", false, err
		}
		return "已开始新聊天", false, nil
	case "ls":
		sessions := s.listSessions(j)
		if len(sessions) == 0 {
			return "当前人设：" + s.promptName(j.cfg.PromptID) + "\n暂无会话，" + formatClawBotStartChatHint(cfg), false, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "当前人设：%s\n会话列表（共 %d 个，当前会话以 * 标记）", s.promptName(j.cfg.PromptID), len(sessions))
		for i, session := range sessions {
			marker := " "
			if current != nil && current.SessionID == session.ID {
				marker = "*"
			}
			fmt.Fprintf(&b, "\n%s %d. %s\n   更新于 %s | ID %s", marker, i+1, formatClawBotSessionTitle(session.Title), formatClawBotSessionTime(session.UpdatedAt), shortClawBotSessionID(session.ID))
		}
		b.WriteString("\n发送 /menu 查看全部命令")
		return b.String(), false, nil
	case "rename":
		if args == "" {
			return "用法：/rename <标题>", false, nil
		}
		if current == nil {
			return "当前没有可重命名的会话，" + formatClawBotStartChatHint(cfg), false, nil
		}
		if err := s.handler.chatManager.UpdateSessionTitle(current.SessionID, args); err != nil {
			return "", false, err
		}
		return "已将当前会话重命名为：" + args, false, nil
	case "checkout", "delete":
		selector := normalizeClawBotCommandSelector(args)
		if selector == "" {
			usage := "/checkout <序号>"
			if command.Name == "delete" {
				usage = "/delete <序号|current>"
			}
			return "用法：" + usage + "\n可先发送 /ls 查看当前人设下的会话列表。", false, nil
		}
		var target storage.ChatSession
		if command.Name == "delete" && isClawBotCurrentSelector(selector) {
			if current == nil {
				return "当前没有可删除的会话，" + formatClawBotStartChatHint(cfg), false, nil
			}
			target = storage.ChatSession{ID: current.SessionID, Title: current.Title}
		} else {
			var err error
			target, _, err = resolveClawBotSessionSelector(s.listSessions(j), selector)
			if err != nil {
				return "未找到唯一对应会话或序号超出范围，可先发送 /ls 查看当前人设下的会话列表。", false, nil
			}
		}
		if record, ok := s.handler.chatManager.GetSession(target.ID); !ok || !qqOfficialSessionMatches(record.SessionID, record.PromptID, j) {
			return "目标会话不可用，可先发送 /ls 刷新当前列表。", false, nil
		}
		if command.Name == "checkout" {
			if err := s.activateSessionLocked(j, target.ID); err != nil {
				return "", false, err
			}
			return "已切换到会话：" + formatClawBotSessionTitle(target.Title), false, nil
		}
		if err := s.handler.chatManager.DeleteSession(target.ID); err != nil {
			return "", false, err
		}
		reply := "已删除会话：" + formatClawBotSessionTitle(target.Title)
		if current != nil && current.SessionID == target.ID {
			next, err := s.currentSessionLocked(j)
			if err != nil {
				return "", false, err
			}
			if next == nil {
				if err := s.activateSessionLocked(j, ""); err != nil {
					return "", false, err
				}
				reply += "\n当前人设下已无剩余会话，下一条消息会开始新会话。"
			} else {
				reply += "\n已切换到最新会话：" + formatClawBotSessionTitle(next.Title)
			}
		}
		return reply, false, nil
	}
	return "用法：/" + command.Name, false, nil
}

func (s *QQOfficialService) promptCommandLocked(j qqOfficialJob, args string) (string, bool, error) {
	selector := normalizeClawBotCommandSelector(args)
	const warning = "\n注意：此命令会切换整个 QQ 官方渠道的人设。"
	if selector == "" {
		return "当前人设：" + s.promptName(j.cfg.PromptID) + "\n发送 /prompt ls 查看人设列表\n发送 /prompt <序号|id> 切换人设\n发送 /prompt default 切换到默认人设" + warning, false, nil
	}
	prompts := s.prompts()
	if isClawBotListSelector(selector) {
		var b strings.Builder
		fmt.Fprintf(&b, "当前人设：%s\n人设列表（当前人设以 * 标记）", s.promptName(j.cfg.PromptID))
		marker := " "
		if j.cfg.PromptID == "" {
			marker = "*"
		}
		fmt.Fprintf(&b, "\n%s 0. 默认", marker)
		for i, prompt := range prompts {
			marker = " "
			if prompt.ID == j.cfg.PromptID {
				marker = "*"
			}
			fmt.Fprintf(&b, "\n%s %d. %s\n   ID %s", marker, i+1, formatClawBotPromptName(prompt), prompt.ID)
		}
		b.WriteString("\n发送 /prompt <序号|id> 切换人设\n发送 /prompt default 切换到默认人设" + warning)
		return b.String(), false, nil
	}
	prompt, _, useDefault, err := resolveClawBotPromptSelector(prompts, selector)
	if err != nil {
		return "未找到唯一对应人设或序号超出范围，可先发送 /prompt ls 查看列表。", false, nil
	}
	id := prompt.ID
	if useDefault {
		id = ""
	}
	if id == j.cfg.PromptID {
		return "当前 QQ 官方渠道已在使用人设：" + s.promptName(id), false, nil
	}
	cfg := j.cfg
	cfg.PromptID = id
	if err := s.handler.configManager.UpdateQQOfficialConfig(cfg); err != nil {
		return "", false, err
	}
	return "已切换 QQ 官方当前人设为：" + s.promptName(id) + "\n下一条消息将继续该人设的当前会话，没有会话时会自动新建。" + warning, true, nil
}

func (s *QQOfficialService) regenerate(j qqOfficialJob, op string) error {
	s.mu.Lock()
	current, err := s.currentSessionLocked(j)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if current == nil {
		return s.commandReply(j, "当前没有可重新生成的会话。")
	}
	snapshot, err := s.handler.chatManager.SnapshotTrailingResponseBatch(current.SessionID)
	if err != nil {
		return err
	}
	if snapshot == nil || snapshot.Session == nil || len(snapshot.TailMessages) == 0 {
		return s.commandReply(j, "当前没有可重新生成的上一轮回复。")
	}
	base := snapshot.Session.Messages
	expected := append(append([]storage.ChatMessage(nil), base...), snapshot.TailMessages...)
	promptName, persona := "", ""
	if j.cfg.PromptID != "" {
		if s.handler.promptManager == nil {
			return fmt.Errorf("configured prompt missing")
		}
		p, ok := s.handler.promptManager.Get(j.cfg.PromptID)
		if !ok {
			return fmt.Errorf("configured prompt missing")
		}
		promptName, persona = p.Name, p.Content
	}
	generationSession := *snapshot.Session
	generationSession.Messages = append([]storage.ChatMessage(nil), base...)
	for i := range generationSession.Messages {
		generationSession.Messages[i].Quote = nil
	}
	ctx, cancel := context.WithTimeout(j.ctx, 2*time.Minute)
	defer cancel()
	originalCtx := j.ctx
	j.ctx = ctx
	reply, err := s.generate(ctx, sessionReplyOptions{
		Session: &generationSession, PromptID: j.cfg.PromptID, PromptName: promptName, Persona: persona,
		Channel: chatToolChannelQQOfficial, ToolOptions: chatToolOptions{Channel: chatToolChannelQQOfficial}, PersistMode: sessionReplyPersistAll,
		ExtraSystemGuides: []string{qqOfficialSystemGuide},
	})
	if err != nil {
		j.ctx = originalCtx
		return s.notifyGenerationFailure(j, current.SessionID, err)
	}
	if reply == nil || reply.NoReply || len(reply.StorageMessages) == 0 {
		return s.commandReply(j, "暂时无法重新生成，请稍后再试。")
	}
	return s.deliverWithBase(j, current.SessionID, op, expected, base, reply, true)
}

const qqOfficialSystemGuide = "你正在通过 QQ 官方机器人私聊渠道回复用户。只支持当前私聊的文字和图片；不能设置提醒、主动问候、访问群或频道，也不能转写语音。未解析的附件仅为占位，不要声称已读取。不要输出路径或 CQ 码代替发送图片。"
