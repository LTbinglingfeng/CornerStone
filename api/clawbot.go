package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	clawBotQRCodeSessionTTL  = 5 * time.Minute
	clawBotConversationIdle  = 30 * time.Minute
	clawBotCleanupInterval   = 5 * time.Minute
	clawBotReplyChunkMaxRune = 2000
	clawBotProcessTimeout    = 2 * time.Minute

	clawBotCommandMenu       = "menu"
	clawBotCommandNew        = "new"
	clawBotCommandList       = "ls"
	clawBotCommandCheckout   = "checkout"
	clawBotCommandRename     = "rename"
	clawBotCommandDelete     = "delete"
	clawBotCommandPrompt     = "prompt"
	clawBotCommandRegenerate = "re"

	clawBotExclusiveOpRegenerate = "regenerate"
)

var (
	errClawBotSelectorAmbiguous       = errors.New("clawbot selector ambiguous")
	errClawBotSelectorIndexOutOfRange = errors.New("clawbot selector index out of range")
)

type ClawBotSettingsResponse struct {
	Enabled            bool            `json:"enabled"`
	BaseURL            string          `json:"base_url"`
	BotToken           string          `json:"bot_token,omitempty"`
	HasBotToken        bool            `json:"has_bot_token"`
	ILinkUserID        string          `json:"ilink_user_id,omitempty"`
	PromptID           string          `json:"prompt_id,omitempty"`
	PromptName         string          `json:"prompt_name,omitempty"`
	CommandPermissions map[string]bool `json:"command_permissions"`
	Status             string          `json:"status"`
	Polling            bool            `json:"polling"`
	LastError          string          `json:"last_error,omitempty"`
	LastErrorAt        string          `json:"last_error_at,omitempty"`
}

type ClawBotQRCodeStartResponse struct {
	SessionID        string `json:"session_id"`
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content,omitempty"`
}

type ClawBotQRCodePollResponse struct {
	Status   string                   `json:"status"`
	Settings *ClawBotSettingsResponse `json:"settings,omitempty"`
}

type clawBotQRCodeSession struct {
	ID               string
	BaseURL          string
	QRCode           string
	QRCodeImgContent string
	CreatedAt        time.Time
}

type clawBotActiveSession struct {
	SessionID  string
	LastActive time.Time
}

type clawBotPendingMessage struct {
	Text       string
	ImagePaths []string
}

type clawBotPendingReply struct {
	Messages        []clawBotPendingMessage
	WindowStartedAt time.Time
	LastActive      time.Time
	Timer           *time.Timer
	Processing      bool
	Ready           bool
	BlockingReason  string
}

type clawBotGeneratedReply struct {
	Text            string
	StorageMessages []storage.ChatMessage
	MemSession      *storage.MemorySession
}

type ClawBotService struct {
	handler *Handler
	client  *client.ClawBotClient

	mu             sync.RWMutex
	qrSessions     map[string]clawBotQRCodeSession
	activeSessions map[string]*clawBotActiveSession
	pendingReplies map[string]*clawBotPendingReply
	contextTokens  map[string]string
	wechatUIN      string

	workerCtx     context.Context
	workerCancel  context.CancelFunc
	workerRunning bool
	lastError     string
	lastErrorAt   time.Time
}

type clawBotCommand struct {
	Name string
	Args string
}

func NewClawBotService(handler *Handler) *ClawBotService {
	wechatUIN, err := client.GenerateClawBotWechatUIN()
	if err != nil {
		logging.Errorf("clawbot generate wechat uin failed: %v", err)
	}

	s := &ClawBotService{
		handler:        handler,
		client:         client.NewClawBotClient(),
		qrSessions:     make(map[string]clawBotQRCodeSession),
		activeSessions: make(map[string]*clawBotActiveSession),
		pendingReplies: make(map[string]*clawBotPendingReply),
		contextTokens:  make(map[string]string),
		wechatUIN:      wechatUIN,
	}
	s.ApplyCurrentConfig()
	go s.cleanupLoop()
	return s
}

func (s *ClawBotService) Close() {
	s.mu.Lock()
	cancel := s.workerCancel
	s.workerCtx = nil
	s.workerCancel = nil
	s.workerRunning = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	s.clearAllPendingReplies()
}

func (s *ClawBotService) ApplyCurrentConfig() {
	cfg := s.handler.configManager.GetClawBotConfig()
	s.applyConfig(cfg)
}

func (s *ClawBotService) applyConfig(cfg config.ClawBotConfig) {
	s.mu.Lock()
	cancel := s.workerCancel
	s.workerCtx = nil
	s.workerCancel = nil
	s.workerRunning = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}

	ctx, workerCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.workerCtx = ctx
	s.workerCancel = workerCancel
	s.workerRunning = true
	s.mu.Unlock()

	go s.pollLoop(ctx, cfg)
}

func (s *ClawBotService) getWorkerCtx() (context.Context, bool) {
	s.mu.RLock()
	ctx := s.workerCtx
	s.mu.RUnlock()
	return ctx, ctx != nil
}

func (s *ClawBotService) StartQRCode(ctx context.Context, baseURL string) (*ClawBotQRCodeStartResponse, error) {
	s.cleanupState()

	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = s.handler.configManager.GetClawBotConfig().BaseURL
	}
	if baseURL == "" {
		baseURL = config.DefaultClawBotBaseURL
	}

	resp, err := s.client.GetBotQRCode(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	qrcode := strings.TrimSpace(resp.QRCode)
	qrcodeImgContent := strings.TrimSpace(resp.QRCodeImgContent)
	if qrcode == "" {
		return nil, fmt.Errorf("clawbot qrcode is empty")
	}

	sessionID := generateID()
	session := clawBotQRCodeSession{
		ID:               sessionID,
		BaseURL:          baseURL,
		QRCode:           qrcode,
		QRCodeImgContent: qrcodeImgContent,
		CreatedAt:        time.Now(),
	}

	s.mu.Lock()
	s.qrSessions[sessionID] = session
	s.mu.Unlock()

	return &ClawBotQRCodeStartResponse{
		SessionID:        sessionID,
		QRCode:           qrcode,
		QRCodeImgContent: qrcodeImgContent,
	}, nil
}

func (s *ClawBotService) PollQRCode(ctx context.Context, sessionID string) (*ClawBotQRCodePollResponse, error) {
	s.cleanupState()

	s.mu.RLock()
	session, ok := s.qrSessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("qrcode session not found")
	}

	resp, err := s.client.GetQRCodeStatus(ctx, session.BaseURL, session.QRCode)
	if err != nil {
		return nil, err
	}

	status := strings.TrimSpace(resp.Status)
	result := &ClawBotQRCodePollResponse{
		Status: status,
	}
	if status == "expired" {
		s.mu.Lock()
		delete(s.qrSessions, sessionID)
		s.mu.Unlock()
		return result, nil
	}
	if status != "confirmed" {
		return result, nil
	}

	cfg := s.handler.configManager.GetClawBotConfig()
	cfg.BaseURL = strings.TrimSpace(resp.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = session.BaseURL
	}
	cfg.BotToken = strings.TrimSpace(resp.BotToken)
	cfg.ILinkUserID = strings.TrimSpace(resp.ILinkUserID)
	cfg.GetUpdatesBuf = ""
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		return nil, err
	}
	s.ApplyCurrentConfig()

	settings, err := s.GetSettings()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.qrSessions, sessionID)
	s.mu.Unlock()
	result.Settings = settings
	return result, nil
}

func (s *ClawBotService) GetSettings() (*ClawBotSettingsResponse, error) {
	cfg := s.handler.configManager.GetClawBotConfig()

	promptName := ""
	if cfg.PromptID != "" {
		if prompt, ok := s.handler.promptManager.Get(cfg.PromptID); ok {
			promptName = prompt.Name
		}
	}

	s.mu.RLock()
	polling := s.workerRunning
	lastError := s.lastError
	lastErrorAt := s.lastErrorAt
	s.mu.RUnlock()

	status := "disabled"
	switch {
	case !cfg.Enabled:
		status = "disabled"
	case strings.TrimSpace(cfg.BotToken) == "":
		status = "missing_token"
	case polling:
		status = "running"
	case lastError != "":
		status = "error"
	default:
		status = "stopped"
	}

	resp := &ClawBotSettingsResponse{
		Enabled:            cfg.Enabled,
		BaseURL:            cfg.BaseURL,
		BotToken:           maskClawBotSecret(cfg.BotToken),
		HasBotToken:        strings.TrimSpace(cfg.BotToken) != "",
		ILinkUserID:        cfg.ILinkUserID,
		PromptID:           cfg.PromptID,
		PromptName:         promptName,
		CommandPermissions: config.NormalizeClawBotCommandPermissions(cfg.CommandPermissions),
		Status:             status,
		Polling:            polling,
		LastError:          lastError,
	}
	if !lastErrorAt.IsZero() {
		resp.LastErrorAt = lastErrorAt.Format(time.RFC3339)
	}
	return resp, nil
}

func (s *ClawBotService) pollLoop(ctx context.Context, cfg config.ClawBotConfig) {
	cursor := strings.TrimSpace(cfg.GetUpdatesBuf)
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		resp, err := s.client.GetUpdates(ctx, cfg.BaseURL, cfg.BotToken, cursor, s.wechatUIN)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.setLastError(err)
			time.Sleep(2 * time.Second)
			continue
		}

		s.clearLastError()
		if resp.ErrCode == -14 {
			cursor = ""
			continue
		}
		if resp.ErrCode != 0 {
			s.setLastError(fmt.Errorf("clawbot getupdates errcode=%d", resp.ErrCode))
			time.Sleep(2 * time.Second)
			continue
		}

		nextCursor := strings.TrimSpace(resp.GetUpdatesBuf)
		if nextCursor != "" {
			cursor = nextCursor
		}

		for _, msg := range resp.Msgs {
			if msg.MessageType != 1 {
				continue
			}
			if strings.TrimSpace(client.ExtractTextFromClawBotMessage(msg)) == "" && len(client.ExtractImageItemsFromClawBotMessage(msg)) == 0 {
				continue
			}
			s.handleIncomingMessage(ctx, cfg, msg)
		}
	}
}

func (s *ClawBotService) handleIncomingMessage(ctx context.Context, cfg config.ClawBotConfig, msg client.ClawBotIncomingMessage) {
	userID := strings.TrimSpace(msg.FromUserID)
	if userID == "" {
		return
	}
	cfg = s.handler.configManager.GetClawBotConfig()

	text := strings.TrimSpace(client.ExtractTextFromClawBotMessage(msg))
	imageItems := client.ExtractImageItemsFromClawBotMessage(msg)
	if text == "" && len(imageItems) == 0 {
		return
	}

	s.setContextToken(userID, msg.ContextToken)
	if len(imageItems) == 0 {
		if escapedText, ok := unescapeClawBotLiteralCommandText(text); ok {
			text = escapedText
		} else if command, ok := parseClawBotCommand(text); ok {
			if !isKnownClawBotCommand(command.Name) {
				s.sendCommandReply(ctx, cfg, userID, formatUnknownClawBotCommandReply(cfg, command.Name), "unknown command")
				return
			}
			if !isClawBotCommandAllowed(cfg, command.Name) {
				s.sendCommandReply(ctx, cfg, userID, formatDisabledClawBotCommandReply(cfg, command.Name), "disabled command")
				return
			}
			if command.Name == clawBotCommandRegenerate {
				s.handleRegenerateCommand(ctx, cfg, userID, command.Args)
				return
			}
			if s.hasExclusiveOperation(userID) {
				s.sendExclusiveBusyReply(ctx, cfg, userID)
				return
			}
			switch command.Name {
			case clawBotCommandMenu:
				s.clearPendingReply(userID)
				s.handleMenuCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandNew:
				s.clearPendingReply(userID)
				s.handleNewCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandList:
				s.clearPendingReply(userID)
				s.handleListCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandCheckout:
				s.clearPendingReply(userID)
				s.handleCheckoutCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandRename:
				s.clearPendingReply(userID)
				s.handleRenameCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandDelete:
				s.clearPendingReply(userID)
				s.handleDeleteCommand(ctx, cfg, userID, command.Args)
				return
			case clawBotCommandPrompt:
				s.clearPendingReply(userID)
				s.handlePromptCommand(ctx, cfg, userID, command.Args)
				return
			}
		}
	}
	if s.hasExclusiveOperation(userID) {
		s.sendExclusiveBusyReply(ctx, cfg, userID)
		return
	}

	imagePaths := s.downloadIncomingImagePaths(ctx, userID, imageItems)
	if text == "" && len(imagePaths) == 0 && len(imageItems) > 0 {
		text = "[用户发送了图片]"
	}
	if text == "" && len(imagePaths) == 0 {
		return
	}

	batch, accepted := s.enqueuePendingMessage(userID, clawBotPendingMessage{
		Text:       text,
		ImagePaths: imagePaths,
	})
	if !accepted {
		s.sendExclusiveBusyReply(ctx, cfg, userID)
		return
	}
	if len(batch) == 0 {
		return
	}
	s.processPendingBatchAsync(ctx, userID, batch)
}

func (s *ClawBotService) downloadIncomingImagePaths(ctx context.Context, userID string, imageItems []*client.ClawBotImageItem) []string {
	if len(imageItems) == 0 {
		return nil
	}

	imagePaths := make([]string, 0, len(imageItems))
	for index, imageItem := range imageItems {
		data, err := s.client.DownloadImageItem(ctx, imageItem)
		if err != nil {
			s.setLastError(err)
			logging.Errorf("clawbot download image failed: user=%s index=%d err=%v", userID, index, err)
			continue
		}

		relPath, err := s.handler.saveCachePhotoBytes(data)
		if err != nil {
			s.setLastError(err)
			logging.Errorf("clawbot save image failed: user=%s index=%d err=%v", userID, index, err)
			continue
		}
		imagePaths = append(imagePaths, relPath)
	}
	return imagePaths
}

func (s *ClawBotService) handleNewCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	if strings.TrimSpace(args) != "" {
		s.sendCommandReply(ctx, cfg, userID, "命令格式不对：/new\n用法：/new", "new usage")
		return
	}

	if _, err := s.createAndActivateSession(userID, cfg.PromptID); err != nil {
		logging.Errorf("clawbot create new session failed: user=%s err=%v", userID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法开始新聊天，请稍后再试。")
		return
	}

	if err := s.sendTextReply(ctx, cfg, userID, "已开始新聊天"); err != nil {
		s.setLastError(err)
		logging.Errorf("clawbot send new session confirmation failed: user=%s err=%v", userID, err)
	}
}

func (s *ClawBotService) handleMenuCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	if strings.TrimSpace(args) != "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/menu", "menu usage")
		return
	}

	s.sendCommandReply(ctx, cfg, userID, s.formatMenuReply(userID, cfg), "menu reply")
}

func (s *ClawBotService) handleListCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	if strings.TrimSpace(args) != "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/ls", "list usage")
		return
	}

	sessions := s.listSessionsForUser(userID, cfg.PromptID)
	reply := s.formatSessionListReply(userID, cfg, sessions)
	s.sendCommandReply(ctx, cfg, userID, reply, "list reply")
}

func (s *ClawBotService) handleCheckoutCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	selector := normalizeClawBotCommandSelector(args)
	if selector == "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/checkout <序号>\n可先发送 /ls 查看当前人设下的会话列表。", "checkout usage")
		return
	}

	sessions := s.listSessionsForUser(userID, cfg.PromptID)
	if len(sessions) == 0 {
		reply := fmt.Sprintf("当前人设 %s 暂无可切换的会话，%s", s.getPromptDisplayName(cfg.PromptID), formatClawBotStartChatHint(cfg))
		s.sendCommandReply(ctx, cfg, userID, reply, "checkout empty")
		return
	}

	session, index, err := resolveClawBotSessionSelector(sessions, selector)
	if err != nil {
		reply := "未找到对应会话，可先发送 /ls 查看当前人设下的会话列表。"
		if isClawBotSelectorAmbiguous(err) {
			reply = "匹配到多个会话，请改用更完整的会话 ID 后缀，或先发送 /ls 查看当前列表。"
		} else if isClawBotSelectorIndexOutOfRange(err) {
			reply = fmt.Sprintf("会话序号超出范围，当前共 %d 个会话。可先发送 /ls 查看列表。", len(sessions))
		}
		s.sendCommandReply(ctx, cfg, userID, reply, "checkout not found")
		return
	}

	record, ok := s.handler.chatManager.GetSession(session.ID)
	if !ok || !s.sessionMatchesUserAndPrompt(record, userID, cfg.PromptID) {
		s.sendCommandReply(ctx, cfg, userID, "目标会话不可用，可先发送 /ls 刷新当前列表。", "checkout unavailable")
		return
	}

	activeID, _ := s.getActiveSessionID(userID)
	s.touchActiveSession(userID, record.SessionID)

	title := formatClawBotSessionTitle(record.Title)
	if activeID == record.SessionID {
		s.sendCommandReply(ctx, cfg, userID, fmt.Sprintf("当前已经在会话 %d：%s", index+1, title), "checkout noop")
		return
	}

	reply := fmt.Sprintf("已切换到会话 %d：%s", index+1, title)
	s.sendCommandReply(ctx, cfg, userID, reply, "checkout reply")
}

func (s *ClawBotService) handleRenameCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	title := strings.TrimSpace(args)
	if title == "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/rename <标题>", "rename usage")
		return
	}

	session, ok := s.getCurrentSessionForUser(userID, cfg.PromptID)
	if !ok {
		reply := "当前没有可重命名的会话，" + formatClawBotStartChatHint(cfg)
		s.sendCommandReply(ctx, cfg, userID, reply, "rename missing session")
		return
	}

	if err := s.handler.chatManager.UpdateSessionTitle(session.SessionID, title); err != nil {
		logging.Errorf("clawbot rename session failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		s.sendCommandReply(ctx, cfg, userID, "暂时无法重命名会话，请稍后再试。", "rename failure")
		return
	}

	finalTitle := title
	if record, ok := s.handler.chatManager.GetSession(session.SessionID); ok && record != nil {
		finalTitle = record.Title
	}
	s.sendCommandReply(ctx, cfg, userID, fmt.Sprintf("已将当前会话重命名为：%s", finalTitle), "rename reply")
}

func (s *ClawBotService) handleDeleteCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	selector := normalizeClawBotCommandSelector(args)
	if selector == "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/delete <序号|current>\n可先发送 /ls 查看当前人设下的会话列表。", "delete usage")
		return
	}

	var target storage.ChatSession
	if isClawBotCurrentSelector(selector) {
		record, ok := s.getCurrentSessionForUser(userID, cfg.PromptID)
		if !ok {
			reply := "当前没有可删除的会话，" + formatClawBotStartChatHint(cfg)
			s.sendCommandReply(ctx, cfg, userID, reply, "delete missing current session")
			return
		}
		target = storage.ChatSession{
			ID:         record.SessionID,
			Title:      record.Title,
			PromptID:   record.PromptID,
			PromptName: record.PromptName,
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
		}
	} else {
		sessions := s.listSessionsForUser(userID, cfg.PromptID)
		if len(sessions) == 0 {
			reply := fmt.Sprintf("当前人设 %s 暂无可删除的会话，%s", s.getPromptDisplayName(cfg.PromptID), formatClawBotStartChatHint(cfg))
			s.sendCommandReply(ctx, cfg, userID, reply, "delete empty")
			return
		}

		session, _, err := resolveClawBotSessionSelector(sessions, selector)
		if err != nil {
			reply := "未找到对应会话，可先发送 /ls 查看当前人设下的会话列表。"
			if isClawBotSelectorAmbiguous(err) {
				reply = "匹配到多个会话，请改用更完整的会话 ID 后缀，或先发送 /ls 查看当前列表。"
			} else if isClawBotSelectorIndexOutOfRange(err) {
				reply = fmt.Sprintf("会话序号超出范围，当前共 %d 个会话。可先发送 /ls 查看列表。", len(sessions))
			}
			s.sendCommandReply(ctx, cfg, userID, reply, "delete not found")
			return
		}
		target = session
	}

	activeID, hasActive := s.getActiveSessionID(userID)
	if err := s.handler.chatManager.DeleteSession(target.ID); err != nil {
		logging.Errorf("clawbot delete session failed: user=%s session=%s err=%v", userID, target.ID, err)
		s.setLastError(err)
		s.sendCommandReply(ctx, cfg, userID, "暂时无法删除会话，请稍后再试。", "delete failure")
		return
	}

	reply := fmt.Sprintf("已删除会话：%s", formatClawBotSessionTitle(target.Title))
	if hasActive && activeID == target.ID {
		if next, ok := s.findLatestSessionForUser(userID, cfg.PromptID); ok {
			s.touchActiveSession(userID, next.SessionID)
			reply += fmt.Sprintf("\n已切换到最新会话：%s", formatClawBotSessionTitle(next.Title))
		} else {
			s.clearActiveSession(userID, target.ID)
			reply += "\n当前人设下已无剩余会话，下一条消息会开始新会话。"
		}
	}

	s.sendCommandReply(ctx, cfg, userID, reply, "delete reply")
}

func (s *ClawBotService) handlePromptCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	selector := normalizeClawBotCommandSelector(args)
	if selector == "" {
		reply := fmt.Sprintf("当前人设：%s\n发送 /prompt ls 查看人设列表\n发送 /prompt <序号|id> 切换人设\n发送 /prompt default 切换到默认人设\n注意：此命令会切换整个 ClawBot 渠道的人设。", s.getPromptDisplayName(cfg.PromptID))
		s.sendCommandReply(ctx, cfg, userID, reply, "prompt usage")
		return
	}

	if isClawBotListSelector(selector) {
		s.sendCommandReply(ctx, cfg, userID, s.formatPromptListReply(cfg.PromptID), "prompt list")
		return
	}

	prompts := s.listPrompts()
	prompt, _, useDefault, err := resolveClawBotPromptSelector(prompts, selector)
	if err != nil {
		reply := "未找到对应人设，可先发送 /prompt ls 查看列表。"
		if isClawBotSelectorAmbiguous(err) {
			reply = "匹配到多个同名人设，请改用人设序号或 ID。可先发送 /prompt ls 查看列表。"
		} else if isClawBotSelectorIndexOutOfRange(err) {
			reply = fmt.Sprintf("人设序号超出范围，当前共 %d 个自定义人设。可先发送 /prompt ls 查看列表。", len(prompts))
		}
		s.sendCommandReply(ctx, cfg, userID, reply, "prompt not found")
		return
	}

	targetPromptID := ""
	targetPromptName := "默认"
	if !useDefault {
		targetPromptID = prompt.ID
		targetPromptName = formatClawBotPromptName(prompt)
	}

	if strings.TrimSpace(cfg.PromptID) == targetPromptID {
		reply := fmt.Sprintf("当前 ClawBot 已在使用人设：%s", targetPromptName)
		s.sendCommandReply(ctx, cfg, userID, reply, "prompt noop")
		return
	}

	cfg.PromptID = targetPromptID
	if err := s.handler.configManager.UpdateClawBotConfig(cfg); err != nil {
		logging.Errorf("clawbot update prompt failed: user=%s prompt=%s err=%v", userID, targetPromptID, err)
		s.setLastError(err)
		s.sendCommandReply(ctx, cfg, userID, "暂时无法切换人设，请稍后再试。", "prompt update failure")
		return
	}

	reply := fmt.Sprintf("已切换 ClawBot 当前人设为：%s", targetPromptName)
	if session, ok := s.findLatestSessionForUser(userID, targetPromptID); ok {
		s.touchActiveSession(userID, session.SessionID)
		reply += fmt.Sprintf("\n已定位到该人设最近会话：%s", formatClawBotSessionTitle(session.Title))
	} else {
		if activeID, ok := s.getActiveSessionID(userID); ok {
			if record, exists := s.handler.chatManager.GetSession(activeID); !exists || !s.sessionMatchesUserAndPrompt(record, userID, targetPromptID) {
				s.clearActiveSession(userID, activeID)
			}
		}
		reply += "\n该人设暂无会话，下一条消息会开始新会话。"
	}
	reply += "\n注意：此变更对当前 ClawBot 渠道生效。"

	s.sendCommandReply(ctx, cfg, userID, reply, "prompt reply")
}

func (s *ClawBotService) handleRegenerateCommand(ctx context.Context, cfg config.ClawBotConfig, userID, args string) {
	if strings.TrimSpace(args) != "" {
		s.sendCommandReply(ctx, cfg, userID, "用法：/re", "regenerate usage")
		return
	}

	if ok, busyReply := s.beginExclusiveOperation(userID, clawBotExclusiveOpRegenerate); !ok {
		s.sendCommandReply(ctx, cfg, userID, busyReply, "regenerate busy")
		return
	}

	go func() {
		defer s.finishExclusiveOperation(userID, clawBotExclusiveOpRegenerate)

		latestCfg := s.handler.configManager.GetClawBotConfig()
		if !latestCfg.Enabled || strings.TrimSpace(latestCfg.BotToken) == "" {
			return
		}

		baseCtx := ctx
		if baseCtx == nil {
			if workerCtx, ok := s.getWorkerCtx(); ok {
				baseCtx = workerCtx
			} else {
				baseCtx = context.Background()
			}
		}

		runCtx, cancel := context.WithTimeout(baseCtx, clawBotProcessTimeout)
		defer cancel()

		s.processRegenerateCommand(runCtx, latestCfg, userID)
	}()
}

func (s *ClawBotService) processRegenerateCommand(ctx context.Context, cfg config.ClawBotConfig, userID string) {
	session, ok := s.getCurrentSessionForUser(userID, cfg.PromptID)
	if !ok {
		reply := "当前没有可重新生成的会话，" + formatClawBotStartChatHint(cfg)
		s.sendCommandReply(ctx, cfg, userID, reply, "regenerate missing session")
		return
	}

	snapshot, err := s.handler.chatManager.SnapshotTrailingResponseBatch(session.SessionID)
	if err != nil {
		logging.Errorf("clawbot regenerate snapshot trailing response batch failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		s.sendCommandReply(ctx, cfg, userID, "暂时无法重新生成，请稍后再试。", "regenerate snapshot failure")
		return
	}
	if snapshot == nil || snapshot.Session == nil || len(snapshot.TailMessages) == 0 {
		s.sendCommandReply(ctx, cfg, userID, "当前没有可重新生成的上一轮回复。", "regenerate empty")
		return
	}

	logging.Infof("clawbot regenerate prepared trailing response replacement with %d messages: user=%s session=%s", len(snapshot.TailMessages), userID, session.SessionID)

	generatedReply, err := s.generateReplyForSession(ctx, snapshot.Session, cfg.PromptID, userID)
	if err != nil {
		logging.Errorf("clawbot regenerate reply failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		s.sendCommandReply(ctx, cfg, userID, "暂时无法重新生成，请稍后再试。", "regenerate reply failure")
		return
	}
	if generatedReply == nil || strings.TrimSpace(generatedReply.Text) == "" {
		s.sendCommandReply(ctx, cfg, userID, "暂时无法重新生成，请稍后再试。", "regenerate empty reply")
		return
	}

	s.sendAndReplaceTrailingReply(ctx, cfg, userID, session.SessionID, generatedReply, snapshot.TailMessages, "regenerate reply")
}

func (s *ClawBotService) processIncomingBatch(ctx context.Context, cfg config.ClawBotConfig, userID string, messages []clawBotPendingMessage) {
	if len(messages) == 0 {
		return
	}

	session, err := s.getOrCreateActiveSession(userID, cfg.PromptID)
	if err != nil {
		logging.Errorf("clawbot prepare session failed: user=%s err=%v", userID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法开始聊天，请稍后再试。")
		return
	}

	now := time.Now()
	storageMessages := make([]storage.ChatMessage, 0, len(messages))
	for index, pendingMessage := range messages {
		trimmed := strings.TrimSpace(pendingMessage.Text)
		if trimmed == "" && len(pendingMessage.ImagePaths) == 0 {
			continue
		}
		storageMessages = append(storageMessages, storage.ChatMessage{
			Role:       "user",
			Content:    trimmed,
			ImagePaths: append([]string(nil), pendingMessage.ImagePaths...),
			Timestamp:  now.Add(time.Millisecond * time.Duration(index)),
		})
	}
	if len(storageMessages) == 0 {
		return
	}

	if err := s.handler.chatManager.AddMessages(session.SessionID, storageMessages); err != nil {
		logging.Errorf("clawbot save user messages failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		_ = s.sendTextReply(ctx, cfg, userID, "暂时无法处理你的消息，请稍后再试。")
		return
	}
	if s.handler.idleGreetingService != nil {
		promptID := strings.TrimSpace(session.PromptID)
		if promptID == "" {
			promptID = strings.TrimSpace(cfg.PromptID)
		}
		promptName := strings.TrimSpace(session.PromptName)
		if promptName == "" && promptID != "" && s.handler.promptManager != nil {
			if prompt, ok := s.handler.promptManager.Get(promptID); ok && prompt != nil {
				promptName = strings.TrimSpace(prompt.Name)
			}
		}
		if _, errIdleGreeting := s.handler.idleGreetingService.Rebuild(idleGreetingScheduleRequest{
			Channel:    storage.ReminderChannelClawBot,
			SessionID:  session.SessionID,
			PromptID:   promptID,
			PromptName: promptName,
			Target: storage.ReminderTarget{
				Kind:   storage.ReminderTargetKindUser,
				UserID: strings.TrimSpace(userID),
			},
			LastUserAt: storageMessages[len(storageMessages)-1].Timestamp,
		}); errIdleGreeting != nil {
			logging.Warnf("rebuild idle greeting failed: channel=clawbot session=%s user=%s err=%v", session.SessionID, userID, errIdleGreeting)
		}
	}

	generatedReply, err := s.generateReply(ctx, session.SessionID, cfg.PromptID, userID)
	if err != nil {
		logging.Errorf("clawbot generate reply failed: user=%s session=%s err=%v", userID, session.SessionID, err)
		s.setLastError(err)
		generatedReply = &clawBotGeneratedReply{Text: "暂时无法处理你的消息，请稍后再试。"}
	}
	s.sendAndPersistReply(ctx, cfg, userID, session.SessionID, generatedReply, "reply")
}

func (s *ClawBotService) generateReply(ctx context.Context, sessionID, configuredPromptID, userID string) (*clawBotGeneratedReply, error) {
	session, ok := s.handler.chatManager.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return s.generateReplyForSession(ctx, session, configuredPromptID, userID)
}

func (s *ClawBotService) generateReplyForSession(ctx context.Context, session *storage.ChatRecord, configuredPromptID, userID string) (*clawBotGeneratedReply, error) {
	if session == nil {
		return nil, fmt.Errorf("session is nil")
	}

	currentConfig := config.DefaultConfig()
	if s.handler.configManager != nil {
		currentConfig = s.handler.configManager.Get()
	}
	normalizedToolToggles := config.NormalizeToolToggles(currentConfig.ToolToggles)

	effectivePromptID := strings.TrimSpace(session.PromptID)
	if effectivePromptID == "" {
		effectivePromptID = strings.TrimSpace(configuredPromptID)
	}
	persona := ""
	validPromptID := ""
	promptName := strings.TrimSpace(session.PromptName)
	if effectivePromptID != "" {
		if prompt, ok := s.handler.promptManager.Get(effectivePromptID); ok {
			persona = strings.TrimSpace(prompt.Content)
			validPromptID = effectivePromptID
			promptName = strings.TrimSpace(prompt.Name)
		}
	}
	memSession := s.handler.getOrCreateMemorySession(validPromptID, session.SessionID)
	writeMemoryEnabled := memSession != nil && isToolEnabledByToggle(normalizedToolToggles, "write_memory")
	availableTools := getChatTools(chatToolOptions{
		ToolToggles:        normalizedToolToggles,
		Channel:            chatToolChannelClawBot,
		WebSearchEnabled:   isWebSearchConfigured(currentConfig),
		WriteMemoryEnabled: writeMemoryEnabled,
	})
	availableToolNames := buildToolNameSet(availableTools)

	channelGuideLines := []string{
		"[渠道说明]",
		"你正在通过微信 ClawBot 渠道回复用户。",
		"你只能回复适合微信文本消息的内容。",
		"默认请直接输出可以发送给用户的自然语言消息。",
	}
	if isToolAvailable(availableToolNames, "no_reply") {
		channelGuideLines = append(channelGuideLines, "如果你决定已读不回，调用 no_reply（可与其它工具一同调用），且不要输出任何可见文本。")
	}
	clawBotToolNames := make([]string, 0, 6)
	for _, name := range []string{"get_time", "get_weather", "web_search", "schedule_reminder", "write_memory", "no_reply"} {
		if isToolAvailable(availableToolNames, name) {
			clawBotToolNames = append(clawBotToolNames, name)
		}
	}
	if len(clawBotToolNames) > 0 {
		channelGuideLines = append(channelGuideLines, "你可以根据需要调用这些工具："+strings.Join(clawBotToolNames, "、")+"。")
	}
	channelGuideLines = append(channelGuideLines, "不要调用红包、拍一拍等微信交互工具。")
	channelGuide := strings.TrimSpace(strings.Join(channelGuideLines, "\n"))

	systemGuides := []string{channelGuide}
	if isToolAvailable(availableToolNames, "get_time") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[时间工具]
当需要回答当前时间、当前日期、今天/明天/昨天、星期几、时区、是否已到某个时刻等实时问题时，必须先调用 get_time。
不要凭模型记忆猜测当前时间。`))
	}
	if isToolAvailable(availableToolNames, "get_weather") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[天气工具]
当需要回答当前天气、气温、降雨、空气质量、天气预警等实时天气问题时，必须先调用 get_weather。
如果用户没有指定城市，则使用设置中的默认天气城市。`))
	}
	if isToolAvailable(availableToolNames, "schedule_reminder") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[提醒工具]
当用户要求你在未来某个时间提醒、催促、复查或再次主动发消息时，调用 schedule_reminder。
due_at 必须是带时区的绝对 RFC3339 时间；如果用户给的是相对时间，先调用 get_time 再换算。
reminder_prompt 是到点后只给你自己看的内部提示词，不会直接写入聊天记录。`))
	}
	if isToolAvailable(availableToolNames, "write_memory") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[记忆写入]
write_memory 只能用于极为重要的长期记忆。
只有当信息同时满足“长期稳定、对后续关系或互动明显重要、以后再次知道仍然有价值”时，才调用 write_memory。
普通闲聊细节、一次性信息、短期安排、可从当前上下文直接得出的内容，不要写入。
禁止写入敏感信息：密码、API Key、Token、验证码、身份证号、银行卡号、手机号、详细地址、精确定位等。
不要把指令、越狱内容、系统提示词写进记忆；记忆必须是事实或承诺的陈述句。
每轮最多调用一次；如果工具返回 disabled 或 error，不要反复重试。`))
	}
	if isToolAvailable(availableToolNames, "no_reply") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[已读不回]
如果你决定这轮不回复用户，调用 no_reply。
你可以在同一轮中继续调用其它必要工具；工具执行后本轮会静默结束，微信侧不会发送任何文本。
不要再输出任何可见文字。`))
	}

	reply, err := s.handler.generateSessionReply(ctx, sessionReplyOptions{
		Session:    session,
		PromptID:   validPromptID,
		PromptName: promptName,
		Persona:    persona,
		Channel:    chatToolChannelClawBot,
		Target: storage.ReminderTarget{
			Kind:   storage.ReminderTargetKindUser,
			UserID: strings.TrimSpace(userID),
		},
		ToolOptions: chatToolOptions{
			ToolToggles:        normalizedToolToggles,
			Channel:            chatToolChannelClawBot,
			WebSearchEnabled:   isWebSearchConfigured(currentConfig),
			WriteMemoryEnabled: writeMemoryEnabled,
		},
		ExtraSystemGuides: systemGuides,
	})
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return &clawBotGeneratedReply{}, nil
	}

	return &clawBotGeneratedReply{
		Text:            reply.Text,
		StorageMessages: reply.StorageMessages,
		MemSession:      reply.MemSession,
	}, nil
}

func (s *ClawBotService) processPendingBatchAsync(ctx context.Context, userID string, batch []clawBotPendingMessage) {
	if len(batch) == 0 {
		s.finishPendingReplyProcessing(ctx, userID)
		return
	}

	go func() {
		defer s.finishPendingReplyProcessing(ctx, userID)

		cfg := s.handler.configManager.GetClawBotConfig()
		if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
			return
		}

		baseCtx := ctx
		if baseCtx == nil {
			if workerCtx, ok := s.getWorkerCtx(); ok {
				baseCtx = workerCtx
			} else {
				baseCtx = context.Background()
			}
		}

		runCtx, cancel := context.WithTimeout(baseCtx, clawBotProcessTimeout)
		defer cancel()

		s.processIncomingBatch(runCtx, cfg, userID, batch)
	}()
}

func (s *ClawBotService) enqueuePendingMessage(userID string, message clawBotPendingMessage) ([]clawBotPendingMessage, bool) {
	mode, delay := s.getReplyWaitWindow()
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.pendingReplies[userID]
	if state == nil {
		state = &clawBotPendingReply{}
		s.pendingReplies[userID] = state
	}
	if state.BlockingReason != "" {
		state.LastActive = now
		return nil, false
	}
	if len(state.Messages) == 0 {
		state.WindowStartedAt = now
	}
	state.Messages = append(state.Messages, message)
	state.LastActive = now

	if delay <= 0 {
		if state.Processing {
			state.Ready = true
			if state.Timer != nil {
				state.Timer.Stop()
				state.Timer = nil
			}
			return nil, true
		}
		return s.beginPendingProcessingLocked(state), true
	}

	s.schedulePendingReplyLocked(userID, state, mode, delay, now)
	return nil, true
}

func (s *ClawBotService) schedulePendingReplyLocked(userID string, state *clawBotPendingReply, mode string, delay time.Duration, now time.Time) {
	if state == nil || len(state.Messages) == 0 {
		return
	}

	fireAt := now.Add(delay)
	if mode == string(config.ReplyWaitWindowModeFixed) {
		startedAt := state.WindowStartedAt
		if startedAt.IsZero() {
			startedAt = now
			state.WindowStartedAt = startedAt
		}
		fireAt = startedAt.Add(delay)
	}

	waitFor := time.Until(fireAt)
	if waitFor < 0 {
		waitFor = 0
	}

	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	state.Timer = time.AfterFunc(waitFor, func() {
		s.flushPendingReply(nil, userID)
	})
}

func (s *ClawBotService) beginPendingProcessingLocked(state *clawBotPendingReply) []clawBotPendingMessage {
	if state == nil || state.Processing || len(state.Messages) == 0 {
		return nil
	}
	batch := append([]clawBotPendingMessage(nil), state.Messages...)
	state.Messages = nil
	state.WindowStartedAt = time.Time{}
	state.Ready = false
	state.Processing = true
	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	return batch
}

func (s *ClawBotService) flushPendingReply(ctx context.Context, userID string) {
	if ctx == nil {
		workerCtx, ok := s.getWorkerCtx()
		if !ok {
			s.clearPendingReply(userID)
			return
		}
		ctx = workerCtx
	}

	s.mu.Lock()
	state := s.pendingReplies[userID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	state.Timer = nil
	if state.Processing {
		state.Ready = true
		s.mu.Unlock()
		return
	}
	batch := s.beginPendingProcessingLocked(state)
	s.mu.Unlock()

	if len(batch) == 0 {
		return
	}
	s.processPendingBatchAsync(ctx, userID, batch)
}

func (s *ClawBotService) finishPendingReplyProcessing(ctx context.Context, userID string) {
	mode, delay := s.getReplyWaitWindow()

	s.mu.Lock()
	state := s.pendingReplies[userID]
	if state == nil {
		s.mu.Unlock()
		return
	}
	state.Processing = false
	shouldFlushNow := state.Ready
	state.Ready = false
	hasMessages := len(state.Messages) > 0
	hasTimer := state.Timer != nil
	if !hasMessages && !hasTimer {
		delete(s.pendingReplies, userID)
	}
	s.mu.Unlock()

	if !hasMessages {
		return
	}
	if shouldFlushNow || delay <= 0 {
		s.flushPendingReply(ctx, userID)
		return
	}
	if hasTimer {
		return
	}

	s.mu.Lock()
	state = s.pendingReplies[userID]
	if state != nil && !state.Processing && len(state.Messages) > 0 {
		s.schedulePendingReplyLocked(userID, state, mode, delay, time.Now())
	}
	s.mu.Unlock()
}

func (s *ClawBotService) clearPendingReply(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state, ok := s.pendingReplies[userID]; ok {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

func (s *ClawBotService) clearAllPendingReplies() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, state := range s.pendingReplies {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

func (s *ClawBotService) getReplyWaitWindow() (string, time.Duration) {
	cfg := s.handler.configManager.Get()
	mode := cfg.ReplyWaitWindowMode
	switch mode {
	case string(config.ReplyWaitWindowModeFixed):
	case string(config.ReplyWaitWindowModeSliding):
	default:
		mode = string(config.ReplyWaitWindowModeSliding)
	}
	seconds := cfg.ReplyWaitWindowSeconds
	if seconds < 0 {
		seconds = 0
	}
	return mode, time.Duration(seconds) * time.Second
}

func (s *ClawBotService) sendTextReply(ctx context.Context, cfg config.ClawBotConfig, userID, text string) error {
	chunks := splitClawBotReplyMessages(text, clawBotReplyChunkMaxRune)
	if len(chunks) == 0 {
		return nil
	}

	contextToken := s.getContextToken(userID)
	for _, chunk := range chunks {
		if err := s.client.SendTextMessage(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, userID, contextToken, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *ClawBotService) sendCommandReply(ctx context.Context, cfg config.ClawBotConfig, userID, text, action string) {
	if err := s.sendTextReply(ctx, cfg, userID, text); err != nil {
		s.setLastError(err)
		logging.Errorf("clawbot send %s failed: user=%s err=%v", action, userID, err)
	}
}

func (s *ClawBotService) sendExclusiveBusyReply(ctx context.Context, cfg config.ClawBotConfig, userID string) {
	s.mu.RLock()
	reason := ""
	if state := s.pendingReplies[userID]; state != nil {
		reason = state.BlockingReason
	}
	s.mu.RUnlock()

	desc, _ := describeClawBotExclusiveOperation(reason)
	s.sendCommandReply(ctx, cfg, userID, fmt.Sprintf("当前正在%s，请等待完成后再发送新消息。", desc), "exclusive busy")
}

func (s *ClawBotService) sendAndPersistReply(ctx context.Context, cfg config.ClawBotConfig, userID, sessionID string, generatedReply *clawBotGeneratedReply, action string) {
	if generatedReply == nil {
		return
	}

	reply := strings.TrimSpace(generatedReply.Text)
	if reply != "" {
		if err := s.sendTextReply(ctx, cfg, userID, reply); err != nil {
			s.setLastError(err)
			logging.Errorf("clawbot send %s failed: user=%s session=%s err=%v", action, userID, sessionID, err)
			return
		}
	}

	messages := generatedReply.StorageMessages
	if len(messages) == 0 && reply != "" {
		messages = []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   reply,
				Timestamp: time.Now(),
			},
		}
	}

	if len(messages) > 0 {
		if err := s.handler.chatManager.AddMessages(sessionID, messages); err != nil {
			logging.Errorf("clawbot save reply batch failed: user=%s session=%s err=%v", userID, sessionID, err)
			s.setLastError(err)
			return
		}
	}

	if generatedReply.MemSession != nil {
		generatedReply.MemSession.OnRoundComplete()
	}
}

func (s *ClawBotService) sendAndReplaceTrailingReply(ctx context.Context, cfg config.ClawBotConfig, userID, sessionID string, generatedReply *clawBotGeneratedReply, expectedTail []storage.ChatMessage, action string) {
	if generatedReply == nil {
		return
	}

	reply := strings.TrimSpace(generatedReply.Text)
	if reply != "" {
		if err := s.sendTextReply(ctx, cfg, userID, reply); err != nil {
			s.setLastError(err)
			logging.Errorf("clawbot send %s failed: user=%s session=%s err=%v", action, userID, sessionID, err)
			return
		}
	}

	replacement := generatedReply.StorageMessages
	if len(replacement) == 0 && reply != "" {
		replacement = []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   reply,
				Timestamp: time.Now(),
			},
		}
	}
	if len(replacement) > 0 {
		if err := s.handler.chatManager.ReplaceTrailingResponseBatch(sessionID, expectedTail, replacement); err != nil {
			logging.Errorf("clawbot replace trailing response batch failed: user=%s session=%s err=%v", userID, sessionID, err)
			s.setLastError(err)
			return
		}
	}

	if generatedReply.MemSession != nil {
		generatedReply.MemSession.OnRoundComplete()
	}
}

func (s *ClawBotService) hasPendingReplyActivity(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.pendingReplies[userID]
	if state == nil {
		return false
	}
	return state.BlockingReason != "" || state.Processing || state.Ready || len(state.Messages) > 0
}

func (s *ClawBotService) getCurrentSessionForUser(userID, promptID string) (*storage.ChatRecord, bool) {
	if sessionID, ok := s.getActiveSessionID(userID); ok {
		if session, exists := s.handler.chatManager.GetSession(sessionID); exists {
			if s.sessionMatchesUserAndPrompt(session, userID, promptID) {
				s.touchActiveSession(userID, sessionID)
				return session, true
			}
		}
	}

	if session, ok := s.findLatestSessionForUser(userID, promptID); ok {
		s.touchActiveSession(userID, session.SessionID)
		return session, true
	}

	return nil, false
}

func (s *ClawBotService) getOrCreateActiveSession(userID, promptID string) (*storage.ChatRecord, error) {
	if session, ok := s.getCurrentSessionForUser(userID, promptID); ok {
		return session, nil
	}

	return s.createAndActivateSession(userID, promptID)
}

func (s *ClawBotService) createAndActivateSession(userID, promptID string) (*storage.ChatRecord, error) {
	session, err := s.createSessionForUser(userID, promptID)
	if err != nil {
		return nil, err
	}
	s.touchActiveSession(userID, session.SessionID)
	return session, nil
}

func (s *ClawBotService) createSessionForUser(userID, promptID string) (*storage.ChatRecord, error) {
	promptID = strings.TrimSpace(promptID)
	promptName := ""
	if promptID != "" {
		if prompt, ok := s.handler.promptManager.Get(promptID); ok {
			promptName = prompt.Name
		} else {
			promptID = ""
		}
	}

	for attempt := 0; attempt < 8; attempt++ {
		sessionID := generateClawBotSessionID(userID)
		if _, exists := s.handler.chatManager.GetSession(sessionID); exists {
			continue
		}
		return s.handler.chatManager.CreateSession(sessionID, "New Chat", promptID, promptName)
	}

	return nil, fmt.Errorf("failed to allocate unique clawbot session id for user %s", userID)
}

func (s *ClawBotService) findLatestSessionForUser(userID, promptID string) (*storage.ChatRecord, bool) {
	sessions := s.listSessionsForUser(userID, promptID)
	if len(sessions) == 0 {
		return nil, false
	}

	record, ok := s.handler.chatManager.GetSession(sessions[0].ID)
	return record, ok
}

func (s *ClawBotService) listSessionsForUser(userID, promptID string) []storage.ChatSession {
	prefix := clawBotSessionPrefix(userID)
	allSessions := s.handler.chatManager.ListSessions()

	filtered := make([]storage.ChatSession, 0, len(allSessions))
	for _, session := range allSessions {
		if !strings.HasPrefix(session.ID, prefix) {
			continue
		}
		if !clawBotSessionMatchesPrompt(session.PromptID, promptID) {
			continue
		}
		filtered = append(filtered, session)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
				return filtered[i].ID < filtered[j].ID
			}
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	return filtered
}

func (s *ClawBotService) sessionMatchesUserAndPrompt(session *storage.ChatRecord, userID, promptID string) bool {
	if session == nil {
		return false
	}
	if !strings.HasPrefix(session.SessionID, clawBotSessionPrefix(userID)) {
		return false
	}
	return clawBotSessionMatchesPrompt(session.PromptID, promptID)
}

func (s *ClawBotService) listPrompts() []storage.Prompt {
	if s.handler == nil || s.handler.promptManager == nil {
		return nil
	}

	prompts := s.handler.promptManager.List()
	sort.Slice(prompts, func(i, j int) bool {
		left := formatClawBotPromptName(prompts[i])
		right := formatClawBotPromptName(prompts[j])
		if strings.EqualFold(left, right) {
			return prompts[i].ID < prompts[j].ID
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})
	return prompts
}

func isKnownClawBotCommand(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/")) {
	case clawBotCommandMenu, clawBotCommandNew, clawBotCommandList, clawBotCommandCheckout, clawBotCommandRename, clawBotCommandDelete, clawBotCommandPrompt, clawBotCommandRegenerate:
		return true
	default:
		return false
	}
}

func isClawBotCommandAllowed(cfg config.ClawBotConfig, name string) bool {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
	if name == clawBotCommandMenu {
		return true
	}
	permissions := config.NormalizeClawBotCommandPermissions(cfg.CommandPermissions)
	allowed, ok := permissions[name]
	if !ok {
		return true
	}
	return allowed
}

func listEnabledClawBotCommands(cfg config.ClawBotConfig) []string {
	commands := []string{clawBotCommandMenu}
	for _, command := range listClawBotKnownCommands() {
		if !isClawBotCommandAllowed(cfg, command) {
			continue
		}
		commands = append(commands, command)
	}
	return commands
}

func formatClawBotStartChatHint(cfg config.ClawBotConfig) string {
	if isClawBotCommandAllowed(cfg, clawBotCommandNew) {
		return "可先发送消息或使用 /new 开始新会话。"
	}
	return "可先直接发送消息开始新会话。"
}

func (s *ClawBotService) formatMenuReply(userID string, cfg config.ClawBotConfig) string {
	promptID := cfg.PromptID
	currentSession := "未开始"
	if session, ok := s.getCurrentSessionForUser(userID, promptID); ok {
		currentSession = formatClawBotSessionTitle(session.Title)
	}

	var builder strings.Builder
	builder.WriteString("[ClawBot 菜单]")
	fmt.Fprintf(&builder, "\n当前人设：%s", s.getPromptDisplayName(promptID))
	fmt.Fprintf(&builder, "\n当前会话：%s", currentSession)
	builder.WriteString("\n/menu 查看此菜单")
	if isClawBotCommandAllowed(cfg, clawBotCommandNew) {
		builder.WriteString("\n/new 开始新聊天")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandList) {
		builder.WriteString("\n/ls 查看当前人设下的会话列表")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandCheckout) {
		builder.WriteString("\n/checkout <序号> 切换会话")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandRegenerate) {
		builder.WriteString("\n/re 重新生成当前会话的上一轮回复")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandRename) {
		builder.WriteString("\n/rename <标题> 重命名当前会话")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandDelete) {
		builder.WriteString("\n/delete <序号|current> 删除会话")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandPrompt) {
		builder.WriteString("\n/prompt 查看当前人设与用法")
		builder.WriteString("\n/prompt ls 查看人设列表")
		builder.WriteString("\n/prompt <序号|id> 切换人设")
		builder.WriteString("\n注意：/prompt 会切换整个 ClawBot 渠道的人设。")
	}
	return builder.String()
}

func (s *ClawBotService) formatSessionListReply(userID string, cfg config.ClawBotConfig, sessions []storage.ChatSession) string {
	promptID := cfg.PromptID
	promptName := s.getPromptDisplayName(promptID)
	if len(sessions) == 0 {
		return fmt.Sprintf("当前人设：%s\n暂无会话，%s", promptName, formatClawBotStartChatHint(cfg))
	}

	activeID, _ := s.getActiveSessionID(userID)
	var builder strings.Builder
	fmt.Fprintf(&builder, "当前人设：%s\n会话列表（共 %d 个，当前会话以 * 标记）", promptName, len(sessions))
	for index, session := range sessions {
		marker := " "
		if session.ID == activeID {
			marker = "*"
		}
		fmt.Fprintf(&builder, "\n%s %d. %s", marker, index+1, formatClawBotSessionTitle(session.Title))
		fmt.Fprintf(&builder, "\n   更新于 %s | ID %s", formatClawBotSessionTime(session.UpdatedAt), shortClawBotSessionID(session.ID))
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandCheckout) {
		builder.WriteString("\n发送 /checkout <序号> 切换会话，例如 /checkout 2")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandRegenerate) {
		builder.WriteString("\n发送 /re 重新生成当前会话的上一轮回复")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandRename) {
		builder.WriteString("\n发送 /rename <标题> 重命名当前会话")
	}
	if isClawBotCommandAllowed(cfg, clawBotCommandDelete) {
		builder.WriteString("\n发送 /delete <序号|current> 删除会话")
	}
	builder.WriteString("\n发送 /menu 查看全部命令")
	return builder.String()
}

func (s *ClawBotService) formatPromptListReply(currentPromptID string) string {
	prompts := s.listPrompts()

	var builder strings.Builder
	fmt.Fprintf(&builder, "当前人设：%s\n人设列表（当前人设以 * 标记）", s.getPromptDisplayName(currentPromptID))

	defaultMarker := " "
	if strings.TrimSpace(currentPromptID) == "" {
		defaultMarker = "*"
	}
	fmt.Fprintf(&builder, "\n%s 0. 默认", defaultMarker)

	for index, prompt := range prompts {
		marker := " "
		if prompt.ID == strings.TrimSpace(currentPromptID) {
			marker = "*"
		}
		fmt.Fprintf(&builder, "\n%s %d. %s", marker, index+1, formatClawBotPromptName(prompt))
		fmt.Fprintf(&builder, "\n   ID %s", prompt.ID)
	}

	builder.WriteString("\n发送 /prompt <序号|id> 切换人设，例如 /prompt 1")
	builder.WriteString("\n发送 /prompt default 切换到默认人设")
	builder.WriteString("\n注意：此命令会切换整个 ClawBot 渠道的人设。")
	return builder.String()
}

func (s *ClawBotService) getPromptDisplayName(promptID string) string {
	promptID = strings.TrimSpace(promptID)
	if promptID == "" {
		return "默认"
	}
	if s.handler == nil || s.handler.promptManager == nil {
		return promptID
	}
	if prompt, ok := s.handler.promptManager.Get(promptID); ok {
		name := strings.TrimSpace(prompt.Name)
		if name != "" {
			return name
		}
	}
	return promptID
}

func (s *ClawBotService) getActiveSessionID(userID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.activeSessions[userID]
	if !ok || state == nil || strings.TrimSpace(state.SessionID) == "" {
		return "", false
	}
	return state.SessionID, true
}

func (s *ClawBotService) touchActiveSession(userID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeSessions == nil {
		s.activeSessions = make(map[string]*clawBotActiveSession)
	}
	s.activeSessions[userID] = &clawBotActiveSession{
		SessionID:  sessionID,
		LastActive: time.Now(),
	}
}

func (s *ClawBotService) clearActiveSession(userID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.activeSessions[userID]
	if !ok || state == nil {
		return
	}
	if strings.TrimSpace(sessionID) != "" && state.SessionID != sessionID {
		return
	}
	delete(s.activeSessions, userID)
}

func (s *ClawBotService) setContextToken(userID, contextToken string) {
	contextToken = strings.TrimSpace(contextToken)
	if contextToken == "" {
		return
	}
	s.mu.Lock()
	s.contextTokens[userID] = contextToken
	s.mu.Unlock()
}

func (s *ClawBotService) getContextToken(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contextTokens[userID]
}

func describeClawBotExclusiveOperation(reason string) (string, string) {
	switch strings.TrimSpace(reason) {
	case clawBotExclusiveOpRegenerate:
		return "重新生成上一轮回复", "/re"
	default:
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return "操作", ""
		}
		return reason, ""
	}
}

func formatClawBotExclusiveOperationBusyReply(blockingReason, attemptedReason string) string {
	blockingDesc, _ := describeClawBotExclusiveOperation(blockingReason)
	_, attemptedCmd := describeClawBotExclusiveOperation(attemptedReason)
	attemptedAction := "执行该操作"
	if attemptedCmd != "" {
		attemptedAction = "使用 " + attemptedCmd
	}
	return fmt.Sprintf("当前正在%s，请等待完成后再%s。", blockingDesc, attemptedAction)
}

func (s *ClawBotService) hasExclusiveOperation(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.pendingReplies[userID]
	return state != nil && state.BlockingReason != ""
}

func (s *ClawBotService) beginExclusiveOperation(userID, reason string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.pendingReplies[userID]
	if state == nil {
		state = &clawBotPendingReply{}
		s.pendingReplies[userID] = state
	}

	switch {
	case state.BlockingReason != "":
		return false, formatClawBotExclusiveOperationBusyReply(state.BlockingReason, reason)
	case state.Processing || state.Ready || len(state.Messages) > 0:
		_, attemptedCmd := describeClawBotExclusiveOperation(reason)
		attemptedAction := "执行该操作"
		if attemptedCmd != "" {
			attemptedAction = "使用 " + attemptedCmd
		}
		return false, fmt.Sprintf("当前还有待处理消息，请等待回复完成后再%s。", attemptedAction)
	}

	state.BlockingReason = reason
	state.LastActive = time.Now()
	return true, ""
}

func (s *ClawBotService) finishExclusiveOperation(userID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.pendingReplies[userID]
	if state == nil || state.BlockingReason != reason {
		return
	}

	state.BlockingReason = ""
	state.LastActive = time.Now()
	if !state.Processing && !state.Ready && len(state.Messages) == 0 && state.Timer == nil {
		delete(s.pendingReplies, userID)
	}
}

func (s *ClawBotService) setLastError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastError = err.Error()
	s.lastErrorAt = time.Now()
	s.mu.Unlock()
}

func (s *ClawBotService) clearLastError() {
	s.mu.Lock()
	s.lastError = ""
	s.lastErrorAt = time.Time{}
	s.mu.Unlock()
}

func (s *ClawBotService) cleanupLoop() {
	ticker := time.NewTicker(clawBotCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupState()
	}
}

func (s *ClawBotService) cleanupState() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.qrSessions {
		if now.Sub(session.CreatedAt) > clawBotQRCodeSessionTTL {
			delete(s.qrSessions, id)
		}
	}
	for userID, session := range s.activeSessions {
		if now.Sub(session.LastActive) > clawBotConversationIdle {
			delete(s.activeSessions, userID)
			delete(s.contextTokens, userID)
		}
	}
	for userID, state := range s.pendingReplies {
		if state == nil {
			delete(s.pendingReplies, userID)
			continue
		}
		if state.Processing || state.BlockingReason != "" {
			continue
		}
		if now.Sub(state.LastActive) <= clawBotConversationIdle {
			continue
		}
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, userID)
	}
}

const clawBotCommandTrimChars = " \t\r\n\v\f\u00a0\u3000\ufeff\u200b\u200c\u200d"
const clawBotCommandTrailingPunctuation = "。．.，,！!？?；;：:、~～"

func parseClawBotCommand(text string) (clawBotCommand, bool) {
	normalized := strings.Trim(text, clawBotCommandTrimChars)
	if normalized == "" {
		return clawBotCommand{}, false
	}
	if strings.HasPrefix(normalized, "／") {
		normalized = "/" + strings.TrimPrefix(normalized, "／")
	}
	if !strings.HasPrefix(normalized, "/") {
		return clawBotCommand{}, false
	}

	body := strings.Trim(normalized[1:], clawBotCommandTrimChars)
	if body == "" {
		return clawBotCommand{}, false
	}

	splitAt := strings.IndexFunc(body, func(r rune) bool {
		return strings.ContainsRune(clawBotCommandTrimChars, r)
	})

	token := body
	args := ""
	if splitAt >= 0 {
		token = body[:splitAt]
		args = strings.Trim(body[splitAt+1:], clawBotCommandTrimChars)
	}

	token = strings.Trim(token, clawBotCommandTrailingPunctuation)
	token = strings.Trim(token, clawBotCommandTrimChars)
	if token == "" {
		return clawBotCommand{}, false
	}

	return clawBotCommand{
		Name: strings.ToLower(token),
		Args: args,
	}, true
}

func isClawBotNewCommand(text string) bool {
	command, ok := parseClawBotCommand(text)
	return ok && command.Name == clawBotCommandNew && command.Args == ""
}

func unescapeClawBotLiteralCommandText(text string) (string, bool) {
	normalized := strings.Trim(text, clawBotCommandTrimChars)
	switch {
	case strings.HasPrefix(normalized, "//"):
		return normalized[1:], true
	case strings.HasPrefix(normalized, "／／"):
		return "/" + strings.TrimPrefix(normalized, "／／"), true
	case strings.HasPrefix(normalized, "/／"):
		return "/" + strings.TrimPrefix(normalized, "/／"), true
	case strings.HasPrefix(normalized, "／/"):
		return "/" + strings.TrimPrefix(normalized, "／/"), true
	default:
		return "", false
	}
}

func listClawBotKnownCommands() []string {
	return []string{
		clawBotCommandNew,
		clawBotCommandList,
		clawBotCommandCheckout,
		clawBotCommandRename,
		clawBotCommandDelete,
		clawBotCommandPrompt,
		clawBotCommandRegenerate,
	}
}

func formatUnknownClawBotCommandReply(cfg config.ClawBotConfig, name string) string {
	normalized := strings.TrimPrefix(strings.TrimSpace(name), "/")
	commandText := "/" + normalized
	var builder strings.Builder
	fmt.Fprintf(&builder, "未知命令：%s", commandText)
	if suggestion := suggestClawBotCommand(listEnabledClawBotCommands(cfg), normalized); suggestion != "" {
		fmt.Fprintf(&builder, "\n你是不是想输入 /%s ？", suggestion)
	}
	builder.WriteString("\n发送 /menu 查看可用命令")
	builder.WriteString("\n如果你想发送以 / 开头的普通内容，请用 // 开头，例如 //menu")
	return builder.String()
}

func formatDisabledClawBotCommandReply(cfg config.ClawBotConfig, name string) string {
	commandText := "/" + strings.TrimPrefix(strings.TrimSpace(name), "/")
	var builder strings.Builder
	fmt.Fprintf(&builder, "命令已禁用：%s", commandText)
	builder.WriteString("\n发送 /menu 查看当前可用命令")
	if suggestion := suggestClawBotCommand(listEnabledClawBotCommands(cfg), name); suggestion != "" {
		fmt.Fprintf(&builder, "\n可用的相近命令：/%s", suggestion)
	}
	return builder.String()
}

func suggestClawBotCommand(candidates []string, name string) string {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/"))
	if name == "" {
		return ""
	}

	best := ""
	bestDistance := -1
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, name) || strings.HasPrefix(name, candidate) {
			return candidate
		}
		distance := levenshteinDistance(name, candidate)
		if bestDistance < 0 || distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}

	if bestDistance >= 0 && bestDistance <= 2 {
		return best
	}
	return ""
}

func levenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}

	prev := make([]int, len(rightRunes)+1)
	curr := make([]int, len(rightRunes)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(leftRunes); i++ {
		curr[0] = i
		for j := 1; j <= len(rightRunes); j++ {
			cost := 0
			if leftRunes[i-1] != rightRunes[j-1] {
				cost = 1
			}
			curr[j] = minClawBotInt(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}

	return prev[len(rightRunes)]
}

func minClawBotInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func normalizeClawBotCommandSelector(selector string) string {
	selector = strings.Trim(selector, clawBotCommandTrimChars)
	selector = strings.Trim(selector, clawBotCommandTrailingPunctuation)
	return strings.Trim(selector, clawBotCommandTrimChars)
}

func isClawBotCurrentSelector(selector string) bool {
	switch strings.ToLower(normalizeClawBotCommandSelector(selector)) {
	case "current", "cur", "this", "active":
		return true
	}
	switch normalizeClawBotCommandSelector(selector) {
	case "当前", "本会话", "当前会话":
		return true
	}
	return false
}

func isClawBotListSelector(selector string) bool {
	switch strings.ToLower(normalizeClawBotCommandSelector(selector)) {
	case clawBotCommandList, "list":
		return true
	}
	return false
}

func isClawBotSelectorIndexOutOfRange(err error) bool {
	return errors.Is(err, errClawBotSelectorIndexOutOfRange)
}

func isClawBotSelectorAmbiguous(err error) bool {
	return errors.Is(err, errClawBotSelectorAmbiguous)
}

func resolveClawBotSessionSelector(sessions []storage.ChatSession, selector string) (storage.ChatSession, int, error) {
	selector = normalizeClawBotCommandSelector(selector)
	if selector == "" {
		return storage.ChatSession{}, -1, fmt.Errorf("empty selector")
	}

	if index, err := strconv.Atoi(selector); err == nil {
		if index < 1 || index > len(sessions) {
			return storage.ChatSession{}, -1, fmt.Errorf("index out of range: %w", errClawBotSelectorIndexOutOfRange)
		}
		return sessions[index-1], index - 1, nil
	}

	for index, session := range sessions {
		if session.ID == selector {
			return session, index, nil
		}
	}

	matchIndex := -1
	for index, session := range sessions {
		if strings.HasSuffix(session.ID, selector) {
			if matchIndex >= 0 {
				return storage.ChatSession{}, -1, fmt.Errorf("ambiguous session selector: %w", errClawBotSelectorAmbiguous)
			}
			matchIndex = index
		}
	}
	if matchIndex >= 0 {
		return sessions[matchIndex], matchIndex, nil
	}

	return storage.ChatSession{}, -1, fmt.Errorf("session not found")
}

func resolveClawBotPromptSelector(prompts []storage.Prompt, selector string) (storage.Prompt, int, bool, error) {
	selector = normalizeClawBotCommandSelector(selector)
	if selector == "" {
		return storage.Prompt{}, -1, false, fmt.Errorf("empty selector")
	}

	switch strings.ToLower(selector) {
	case "0", "default", "none":
		return storage.Prompt{}, -1, true, nil
	}
	switch selector {
	case "默认":
		return storage.Prompt{}, -1, true, nil
	}

	if index, err := strconv.Atoi(selector); err == nil {
		if index < 1 || index > len(prompts) {
			return storage.Prompt{}, -1, false, fmt.Errorf("index out of range: %w", errClawBotSelectorIndexOutOfRange)
		}
		return prompts[index-1], index - 1, false, nil
	}

	for index, prompt := range prompts {
		if prompt.ID == selector {
			return prompt, index, false, nil
		}
	}

	matchIndex := -1
	for index, prompt := range prompts {
		if strings.EqualFold(strings.TrimSpace(prompt.Name), selector) {
			if matchIndex >= 0 {
				return storage.Prompt{}, -1, false, fmt.Errorf("ambiguous prompt selector: %w", errClawBotSelectorAmbiguous)
			}
			matchIndex = index
		}
	}
	if matchIndex >= 0 {
		return prompts[matchIndex], matchIndex, false, nil
	}

	return storage.Prompt{}, -1, false, fmt.Errorf("prompt not found")
}

func clawBotSessionMatchesPrompt(sessionPromptID, promptID string) bool {
	return strings.TrimSpace(sessionPromptID) == strings.TrimSpace(promptID)
}

func formatClawBotPromptName(prompt storage.Prompt) string {
	name := strings.TrimSpace(prompt.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(prompt.ID)
}

func formatClawBotSessionTitle(title string) string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" {
		return "New Chat"
	}

	runes := []rune(title)
	if len(runes) > 36 {
		return string(runes[:36]) + "..."
	}
	return title
}

func formatClawBotSessionTime(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "未知时间"
	}
	return updatedAt.Local().Format("2006-01-02 15:04")
}

func shortClawBotSessionID(sessionID string) string {
	if len(sessionID) <= 12 {
		return sessionID
	}
	return sessionID[len(sessionID)-12:]
}

func clawBotSessionPrefix(userID string) string {
	hash := sha1.Sum([]byte(strings.TrimSpace(userID)))
	return "clawbot_" + hex.EncodeToString(hash[:8]) + "_"
}

func generateClawBotSessionID(userID string) string {
	return clawBotSessionPrefix(userID) + generateID()
}

func newAIClientForProvider(provider *config.Provider) client.AIClient {
	switch provider.Type {
	case config.ProviderTypeOpenAIResponse:
		return client.NewResponsesClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeGemini:
		return client.NewGeminiClient(provider.BaseURL, provider.APIKey)
	case config.ProviderTypeAnthropic:
		return client.NewAnthropicClient(provider.BaseURL, provider.APIKey)
	default:
		return client.NewClient(provider.BaseURL, provider.APIKey)
	}
}

func splitClawBotReply(text string, maxRunes int) []string {
	return splitTextByMaxRunes(text, maxRunes)
}

func splitClawBotReplyMessages(text string, maxRunes int) []string {
	return splitAssistantReplyMessages(text, maxRunes)
}

func maskClawBotSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
