package api

import (
	"context"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	napCatConversationIdle  = 30 * time.Minute
	napCatCleanupInterval   = 5 * time.Minute
	napCatReplyChunkMaxRune = 2000
	napCatProcessTimeout    = 2 * time.Minute
	napCatActionTimeout     = 10 * time.Second
)

type NapCatSettingsResponse struct {
	Enabled               bool     `json:"enabled"`
	AccessToken           string   `json:"access_token,omitempty"`
	HasAccessToken        bool     `json:"has_access_token"`
	PromptID              string   `json:"prompt_id,omitempty"`
	PromptName            string   `json:"prompt_name,omitempty"`
	AllowPrivate          bool     `json:"allow_private"`
	AllowGroup            bool     `json:"allow_group"`
	SourceFilterMode      string   `json:"source_filter_mode"`
	AllowedPrivateUserIDs []string `json:"allowed_private_user_ids,omitempty"`
	AllowedGroupIDs       []string `json:"allowed_group_ids,omitempty"`
	AllowedGroupUserIDs   []string `json:"allowed_group_user_ids,omitempty"`
	Status                string   `json:"status"`
	SelfID                string   `json:"self_id,omitempty"`
	Nickname              string   `json:"nickname,omitempty"`
	LastError             string   `json:"last_error,omitempty"`
	LastErrorAt           string   `json:"last_error_at,omitempty"`
}

type napCatChatSource struct {
	Kind    string
	SelfID  int64
	UserID  int64
	GroupID int64
}

func (s napCatChatSource) isPrivate() bool {
	return s.Kind == "private"
}

type napCatActiveSession struct {
	SessionID  string
	LastActive time.Time
}

type napCatPendingMessage struct {
	Text string
}

type napCatPendingReply struct {
	Messages        []napCatPendingMessage
	WindowStartedAt time.Time
	LastActive      time.Time
	Timer           *time.Timer
	Processing      bool
	Ready           bool
}

type napCatGeneratedReply struct {
	Text            string
	StorageMessages []storage.ChatMessage
	MemSession      *storage.MemorySession
}

type napCatActionRequest struct {
	Action string      `json:"action"`
	Params interface{} `json:"params,omitempty"`
	Echo   string      `json:"echo,omitempty"`
}

type napCatActionResponse struct {
	Status  string          `json:"status,omitempty"`
	RetCode int             `json:"retcode,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Echo    string          `json:"echo,omitempty"`
	Message string          `json:"message,omitempty"`
}

type napCatLoginInfo struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
}

type napCatMessageEvent struct {
	Time        int64           `json:"time"`
	SelfID      int64           `json:"self_id"`
	PostType    string          `json:"post_type"`
	MessageType string          `json:"message_type"`
	SubType     string          `json:"sub_type"`
	MessageID   int64           `json:"message_id"`
	UserID      int64           `json:"user_id"`
	RawMessage  string          `json:"raw_message"`
	Message     json.RawMessage `json:"message"`
	GroupID     int64           `json:"group_id,omitempty"`
}

type napCatMessageSegment struct {
	Type string `json:"type"`
	Data struct {
		Text string `json:"text,omitempty"`
	} `json:"data"`
}

type NapCatService struct {
	handler *Handler

	mu         sync.RWMutex
	conn       *websocket.Conn
	connCancel context.CancelFunc
	writeMu    sync.Mutex

	connectedAt           time.Time
	connectedAccessToken  string
	selfID                int64
	selfNickname          string
	lastError             string
	lastErrorAt           time.Time
	pendingActionWaiters  map[string]chan *napCatActionResponse
	activeSessions        map[napCatChatSource]*napCatActiveSession
	pendingReplies        map[napCatChatSource]*napCatPendingReply
	cleanupDone           chan struct{}
	cleanupPendingStopper sync.Once
}

func NewNapCatService(handler *Handler) *NapCatService {
	service := &NapCatService{
		handler:              handler,
		pendingActionWaiters: make(map[string]chan *napCatActionResponse),
		activeSessions:       make(map[napCatChatSource]*napCatActiveSession),
		pendingReplies:       make(map[napCatChatSource]*napCatPendingReply),
		cleanupDone:          make(chan struct{}),
	}
	go service.cleanupLoop()
	return service
}

func (s *NapCatService) Close() {
	s.cleanupPendingStopper.Do(func() {
		close(s.cleanupDone)
	})
	s.disconnect("service closing")
	s.clearAllPendingReplies()
}

func (s *NapCatService) ApplyCurrentConfig() {
	cfg := config.NapCatConfig{}
	if s.handler != nil && s.handler.configManager != nil {
		cfg = s.handler.configManager.GetNapCatConfig()
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.AccessToken) == "" {
		s.disconnect("napcat disabled or missing token")
		return
	}

	s.mu.RLock()
	tokenAtConnect := s.connectedAccessToken
	s.mu.RUnlock()
	if tokenAtConnect != "" && tokenAtConnect != strings.TrimSpace(cfg.AccessToken) {
		s.disconnect("napcat access token rotated")
	}
}

func (s *NapCatService) GetSettings() (*NapCatSettingsResponse, error) {
	if s.handler == nil || s.handler.configManager == nil {
		return nil, errors.New("config manager not configured")
	}

	cfg := s.handler.configManager.GetNapCatConfig()

	promptName := ""
	if strings.TrimSpace(cfg.PromptID) != "" && s.handler.promptManager != nil {
		if prompt, ok := s.handler.promptManager.Get(cfg.PromptID); ok {
			promptName = strings.TrimSpace(prompt.Name)
		}
	}

	s.mu.RLock()
	connected := s.conn != nil
	selfID := s.selfID
	nickname := s.selfNickname
	lastError := s.lastError
	lastErrorAt := s.lastErrorAt
	s.mu.RUnlock()

	status := "disabled"
	switch {
	case !cfg.Enabled:
		status = "disabled"
	case strings.TrimSpace(cfg.AccessToken) == "":
		status = "missing_token"
	case connected:
		status = "connected"
	case lastError != "":
		status = "error"
	default:
		status = "disconnected"
	}

	resp := &NapCatSettingsResponse{
		Enabled:               cfg.Enabled,
		AccessToken:           maskNapCatSecret(cfg.AccessToken),
		HasAccessToken:        strings.TrimSpace(cfg.AccessToken) != "",
		PromptID:              strings.TrimSpace(cfg.PromptID),
		PromptName:            promptName,
		AllowPrivate:          cfg.AllowPrivate,
		AllowGroup:            cfg.AllowGroup,
		SourceFilterMode:      strings.TrimSpace(cfg.SourceFilterMode),
		AllowedPrivateUserIDs: append([]string(nil), cfg.AllowedPrivateUserIDs...),
		AllowedGroupIDs:       append([]string(nil), cfg.AllowedGroupIDs...),
		AllowedGroupUserIDs:   append([]string(nil), cfg.AllowedGroupUserIDs...),
		Status:                status,
		LastError:             lastError,
	}
	if selfID != 0 {
		resp.SelfID = fmt.Sprintf("%d", selfID)
	}
	if strings.TrimSpace(nickname) != "" {
		resp.Nickname = strings.TrimSpace(nickname)
	}
	if !lastErrorAt.IsZero() {
		resp.LastErrorAt = lastErrorAt.Format(time.RFC3339)
	}
	return resp, nil
}

func (s *NapCatService) Connect(conn *websocket.Conn, accessToken string) {
	if conn == nil {
		return
	}

	accessToken = strings.TrimSpace(accessToken)

	s.mu.Lock()
	prevConn := s.conn
	prevCancel := s.connCancel
	s.conn = conn
	s.connectedAt = time.Now()
	s.connectedAccessToken = accessToken
	s.connCancel = nil
	s.selfID = 0
	s.selfNickname = ""
	s.clearLastErrorLocked()
	s.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	if prevConn != nil {
		_ = prevConn.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.conn == conn {
		s.connCancel = cancel
	} else {
		cancel()
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	go s.readLoop(ctx, conn)

	go func() {
		if err := s.refreshLoginInfo(ctx); err != nil {
			s.setLastError(err)
			logging.Warnf("napcat get_login_info failed: err=%v", err)
		}
	}()
}

func (s *NapCatService) disconnect(reason string) {
	s.mu.Lock()
	conn := s.conn
	cancel := s.connCancel
	s.conn = nil
	s.connCancel = nil
	s.connectedAt = time.Time{}
	s.connectedAccessToken = ""
	waiters := s.pendingActionWaiters
	s.pendingActionWaiters = make(map[string]chan *napCatActionResponse)
	s.mu.Unlock()

	if reason != "" {
		logging.Infof("napcat disconnect: %s", reason)
	}
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	for _, ch := range waiters {
		close(ch)
	}
}

func (s *NapCatService) clearLastErrorLocked() {
	s.lastError = ""
	s.lastErrorAt = time.Time{}
}

func (s *NapCatService) setLastError(err error) {
	if err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return
	}
	s.mu.Lock()
	s.lastError = msg
	s.lastErrorAt = time.Now()
	s.mu.Unlock()
}

func (s *NapCatService) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		s.mu.Lock()
		shouldDisconnect := s.conn == conn
		s.mu.Unlock()
		if shouldDisconnect {
			s.disconnect("connection closed")
		}
	}()

	conn.SetReadLimit(8 << 20) // 8MB

	for {
		if ctx.Err() != nil {
			return
		}

		_, data, errRead := conn.ReadMessage()
		if errRead != nil {
			if ctx.Err() != nil {
				return
			}
			if websocket.IsCloseError(errRead, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			s.setLastError(errRead)
			logging.Warnf("napcat ws read failed: err=%v", errRead)
			return
		}

		s.handleIncomingFrame(ctx, data)
	}
}

func (s *NapCatService) handleIncomingFrame(ctx context.Context, data []byte) {
	if len(data) == 0 {
		return
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		logging.Warnf("napcat ws ignore invalid json: err=%v", err)
		return
	}

	if rawPostType, ok := envelope["post_type"]; ok && len(rawPostType) > 0 {
		var event napCatMessageEvent
		if err := json.Unmarshal(data, &event); err != nil {
			logging.Warnf("napcat ws decode event failed: err=%v", err)
			return
		}
		if event.SelfID != 0 {
			s.mu.Lock()
			if s.selfID == 0 {
				s.selfID = event.SelfID
			}
			s.mu.Unlock()
		}
		s.handleEvent(ctx, event)
		return
	}

	echo := ""
	if rawEcho, ok := envelope["echo"]; ok && len(rawEcho) > 0 {
		_ = json.Unmarshal(rawEcho, &echo)
		echo = strings.TrimSpace(echo)
	}
	if echo == "" {
		return
	}

	var resp napCatActionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		logging.Warnf("napcat ws decode action response failed: err=%v", err)
		return
	}

	s.mu.Lock()
	ch := s.pendingActionWaiters[echo]
	delete(s.pendingActionWaiters, echo)
	s.mu.Unlock()

	if ch == nil {
		return
	}
	select {
	case ch <- &resp:
	default:
	}
	close(ch)
}

func (s *NapCatService) refreshLoginInfo(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, napCatActionTimeout)
	defer cancel()

	resp, err := s.sendActionWithEcho(runCtx, "get_login_info", nil)
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("missing get_login_info response")
	}
	if strings.ToLower(strings.TrimSpace(resp.Status)) != "ok" || resp.RetCode != 0 {
		return fmt.Errorf("get_login_info failed: status=%s retcode=%d message=%s", resp.Status, resp.RetCode, strings.TrimSpace(resp.Message))
	}

	var info napCatLoginInfo
	if len(resp.Data) == 0 {
		return errors.New("get_login_info response missing data")
	}
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return fmt.Errorf("parse get_login_info data failed: %w", err)
	}

	s.mu.Lock()
	s.selfID = info.UserID
	s.selfNickname = strings.TrimSpace(info.Nickname)
	s.mu.Unlock()
	return nil
}

func (s *NapCatService) sendActionWithEcho(ctx context.Context, action string, params interface{}) (*napCatActionResponse, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		return nil, errors.New("action is empty")
	}

	echo := generateID()
	waiter := make(chan *napCatActionResponse, 1)

	s.mu.Lock()
	conn := s.conn
	if conn == nil {
		s.mu.Unlock()
		return nil, errors.New("napcat websocket not connected")
	}
	if s.pendingActionWaiters == nil {
		s.pendingActionWaiters = make(map[string]chan *napCatActionResponse)
	}
	s.pendingActionWaiters[echo] = waiter
	s.mu.Unlock()

	req := napCatActionRequest{
		Action: action,
		Params: params,
		Echo:   echo,
	}
	if err := s.writeJSON(conn, req); err != nil {
		s.mu.Lock()
		if ch := s.pendingActionWaiters[echo]; ch != nil {
			delete(s.pendingActionWaiters, echo)
			close(ch)
		}
		s.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-waiter:
		if resp == nil {
			return nil, errors.New("napcat websocket closed")
		}
		return resp, nil
	case <-ctx.Done():
		s.mu.Lock()
		if ch := s.pendingActionWaiters[echo]; ch != nil {
			delete(s.pendingActionWaiters, echo)
			close(ch)
		}
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *NapCatService) sendAction(ctx context.Context, action string, params interface{}) error {
	_ = ctx
	action = strings.TrimSpace(action)
	if action == "" {
		return errors.New("action is empty")
	}

	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return errors.New("napcat websocket not connected")
	}

	return s.writeJSON(conn, napCatActionRequest{
		Action: action,
		Params: params,
	})
}

func (s *NapCatService) writeJSON(conn *websocket.Conn, value interface{}) error {
	if conn == nil {
		return errors.New("napcat websocket not connected")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(value); err != nil {
		s.setLastError(err)
		return err
	}
	return nil
}

func (s *NapCatService) handleEvent(ctx context.Context, event napCatMessageEvent) {
	if strings.TrimSpace(event.PostType) != "message" {
		return
	}
	if strings.TrimSpace(event.MessageType) != "private" {
		return
	}

	cfg := config.NapCatConfig{}
	if s.handler != nil && s.handler.configManager != nil {
		cfg = s.handler.configManager.GetNapCatConfig()
	}
	if !cfg.Enabled {
		return
	}
	if !cfg.AllowPrivate {
		return
	}

	if event.UserID == 0 {
		return
	}
	if event.SelfID != 0 && event.UserID == event.SelfID {
		return
	}
	s.mu.RLock()
	selfID := s.selfID
	s.mu.RUnlock()
	if selfID != 0 && event.UserID == selfID {
		return
	}

	text := extractNapCatPlainText(event)
	if text == "" {
		return
	}

	source := napCatChatSource{
		Kind:   "private",
		SelfID: event.SelfID,
		UserID: event.UserID,
	}
	if !napCatSourceAllowed(cfg, source) {
		return
	}

	batch, accepted := s.enqueuePendingMessage(source, napCatPendingMessage{Text: text})
	if !accepted {
		return
	}
	if len(batch) == 0 {
		return
	}
	s.processPendingBatchAsync(ctx, source, batch)
}

func extractNapCatPlainText(event napCatMessageEvent) string {
	if text, ok := extractNapCatTextFromMessage(event.Message); ok {
		return text
	}
	if len(event.Message) != 0 {
		// When structured message payload exists, only accept pure-text messages.
		return ""
	}
	return extractNapCatRawText(event.RawMessage)
}

func extractNapCatTextFromMessage(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		return text, text != ""
	}

	var segments []napCatMessageSegment
	if err := json.Unmarshal(raw, &segments); err != nil {
		return "", false
	}
	if len(segments) == 0 {
		return "", false
	}

	var builder strings.Builder
	for _, segment := range segments {
		if strings.TrimSpace(segment.Type) != "text" {
			return "", false
		}
		builder.WriteString(segment.Data.Text)
	}
	text = strings.TrimSpace(builder.String())
	return text, text != ""
}

func extractNapCatRawText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if strings.Contains(text, "[CQ:") {
		return ""
	}
	return text
}

func napCatSourceAllowed(cfg config.NapCatConfig, source napCatChatSource) bool {
	mode := strings.TrimSpace(cfg.SourceFilterMode)
	if mode == "" || mode == "all" {
		return true
	}
	if mode != "allowlist" {
		return true
	}

	switch {
	case source.isPrivate():
		userIDStr := fmt.Sprintf("%d", source.UserID)
		if strings.TrimSpace(userIDStr) == "" {
			return false
		}
		return stringSliceContains(cfg.AllowedPrivateUserIDs, userIDStr)
	case source.Kind == "group":
		groupIDStr := fmt.Sprintf("%d", source.GroupID)
		if strings.TrimSpace(groupIDStr) == "" || !stringSliceContains(cfg.AllowedGroupIDs, groupIDStr) {
			return false
		}
		if len(cfg.AllowedGroupUserIDs) == 0 {
			return true
		}
		userIDStr := fmt.Sprintf("%d", source.UserID)
		if strings.TrimSpace(userIDStr) == "" {
			return false
		}
		return stringSliceContains(cfg.AllowedGroupUserIDs, userIDStr)
	default:
		return false
	}
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (s *NapCatService) processPendingBatchAsync(ctx context.Context, source napCatChatSource, batch []napCatPendingMessage) {
	if len(batch) == 0 {
		s.finishPendingReplyProcessing(ctx, source)
		return
	}

	go func() {
		defer s.finishPendingReplyProcessing(ctx, source)

		cfg := config.NapCatConfig{}
		if s.handler != nil && s.handler.configManager != nil {
			cfg = s.handler.configManager.GetNapCatConfig()
		}
		if !cfg.Enabled || strings.TrimSpace(cfg.AccessToken) == "" {
			return
		}

		baseCtx := ctx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		runCtx, cancel := context.WithTimeout(baseCtx, napCatProcessTimeout)
		defer cancel()

		s.processIncomingBatch(runCtx, cfg, source, batch)
	}()
}

func (s *NapCatService) processIncomingBatch(ctx context.Context, cfg config.NapCatConfig, source napCatChatSource, messages []napCatPendingMessage) {
	if len(messages) == 0 || !source.isPrivate() {
		return
	}

	session, err := s.getOrCreateActiveSession(source, cfg.PromptID)
	if err != nil {
		logging.Errorf("napcat prepare session failed: source=%+v err=%v", source, err)
		s.setLastError(err)
		_ = s.sendPrivateText(ctx, source.UserID, "暂时无法开始聊天，请稍后再试。")
		return
	}

	now := time.Now()
	storageMessages := make([]storage.ChatMessage, 0, len(messages))
	for index, pendingMessage := range messages {
		trimmed := strings.TrimSpace(pendingMessage.Text)
		if trimmed == "" {
			continue
		}
		storageMessages = append(storageMessages, storage.ChatMessage{
			Role:      "user",
			Content:   trimmed,
			Timestamp: now.Add(time.Millisecond * time.Duration(index)),
		})
	}
	if len(storageMessages) == 0 {
		return
	}

	if err := s.handler.chatManager.AddMessages(session.SessionID, storageMessages); err != nil {
		logging.Errorf("napcat save user messages failed: source=%+v session=%s err=%v", source, session.SessionID, err)
		s.setLastError(err)
		_ = s.sendPrivateText(ctx, source.UserID, "暂时无法处理你的消息，请稍后再试。")
		return
	}

	generatedReply, err := s.generateReply(ctx, session.SessionID, cfg.PromptID)
	if err != nil {
		logging.Errorf("napcat generate reply failed: source=%+v session=%s err=%v", source, session.SessionID, err)
		s.setLastError(err)
		generatedReply = &napCatGeneratedReply{Text: "暂时无法处理你的消息，请稍后再试。"}
	}
	s.sendAndPersistReply(ctx, source, session.SessionID, generatedReply)
}

func (s *NapCatService) generateReply(ctx context.Context, sessionID, configuredPromptID string) (*napCatGeneratedReply, error) {
	session, ok := s.handler.chatManager.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return s.generateReplyForSession(ctx, session, configuredPromptID)
}

func (s *NapCatService) generateReplyForSession(ctx context.Context, session *storage.ChatRecord, configuredPromptID string) (*napCatGeneratedReply, error) {
	if session == nil {
		return nil, fmt.Errorf("session is nil")
	}

	currentConfig := config.DefaultConfig()
	if s.handler != nil && s.handler.configManager != nil {
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
		Channel:            chatToolChannelNapCat,
		WebSearchEnabled:   isWebSearchConfigured(currentConfig),
		WriteMemoryEnabled: writeMemoryEnabled,
	})
	availableToolNames := buildToolNameSet(availableTools)

	channelGuideLines := []string{
		"[渠道说明]",
		"你正在通过 QQ NapCat 私聊渠道回复用户。",
		"你只能回复适合即时聊天窗口发送的纯文本内容。",
		"尽量使用自然语言短句，不要输出大段 Markdown、表格或代码块。",
	}
	if len(availableToolNames) > 0 {
		allowed := make([]string, 0, 4)
		for _, name := range []string{"get_time", "get_weather", "web_search", "write_memory"} {
			if isToolAvailable(availableToolNames, name) {
				allowed = append(allowed, name)
			}
		}
		if len(allowed) > 0 {
			channelGuideLines = append(channelGuideLines, "你可以根据需要调用这些工具："+strings.Join(allowed, "、")+"。")
		}
	}
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
	if isToolAvailable(availableToolNames, "web_search") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[网络搜索]
当需要查事实、资料、百科、新闻等外部信息时，调用 web_search。`))
	}
	if isToolAvailable(availableToolNames, "write_memory") {
		systemGuides = append(systemGuides, strings.TrimSpace(`[记忆写入]
write_memory 只能用于极为重要的长期记忆，禁止写入敏感信息。宁可少写，不要滥写。`))
	}

	reply, err := s.handler.generateSessionReply(ctx, sessionReplyOptions{
		Session:    session,
		PromptID:   validPromptID,
		PromptName: promptName,
		Persona:    persona,
		Channel:    chatToolChannelNapCat,
		ToolOptions: chatToolOptions{
			Channel:            chatToolChannelNapCat,
			WebSearchEnabled:   isWebSearchConfigured(currentConfig),
			WriteMemoryEnabled: writeMemoryEnabled,
		},
		ExtraSystemGuides: systemGuides,
	})
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return &napCatGeneratedReply{}, nil
	}

	return &napCatGeneratedReply{
		Text:            reply.Text,
		StorageMessages: reply.StorageMessages,
		MemSession:      reply.MemSession,
	}, nil
}

func (s *NapCatService) sendAndPersistReply(ctx context.Context, source napCatChatSource, sessionID string, generatedReply *napCatGeneratedReply) {
	if generatedReply == nil || !source.isPrivate() {
		return
	}

	reply := strings.TrimSpace(generatedReply.Text)
	if reply != "" {
		if err := s.sendPrivateText(ctx, source.UserID, reply); err != nil {
			s.setLastError(err)
			logging.Errorf("napcat send reply failed: source=%+v session=%s err=%v", source, sessionID, err)
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
			logging.Errorf("napcat save reply batch failed: source=%+v session=%s err=%v", source, sessionID, err)
			s.setLastError(err)
			return
		}
	}

	if generatedReply.MemSession != nil {
		generatedReply.MemSession.OnRoundComplete()
	}
}

func (s *NapCatService) sendPrivateText(ctx context.Context, userID int64, text string) error {
	chunks := splitAssistantReplyMessages(text, napCatReplyChunkMaxRune)
	if len(chunks) == 0 {
		return nil
	}

	for _, chunk := range chunks {
		if err := s.sendConfirmedAction(ctx, "send_private_msg", map[string]interface{}{
			"user_id": userID,
			"message": chunk,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *NapCatService) sendConfirmedAction(ctx context.Context, action string, params interface{}) error {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		timeoutCtx, cancel := context.WithTimeout(runCtx, napCatActionTimeout)
		defer cancel()
		runCtx = timeoutCtx
	}

	resp, err := s.sendActionWithEcho(runCtx, action, params)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("%s failed: empty response", action)
	}
	if strings.ToLower(strings.TrimSpace(resp.Status)) != "ok" || resp.RetCode != 0 {
		return fmt.Errorf("%s failed: status=%s retcode=%d message=%s", action, strings.TrimSpace(resp.Status), resp.RetCode, strings.TrimSpace(resp.Message))
	}
	return nil
}

func (s *NapCatService) getOrCreateActiveSession(source napCatChatSource, promptID string) (*storage.ChatRecord, error) {
	if session, ok := s.getCurrentSessionForSource(source, promptID); ok {
		return session, nil
	}
	return s.createAndActivateSession(source, promptID)
}

func (s *NapCatService) getCurrentSessionForSource(source napCatChatSource, promptID string) (*storage.ChatRecord, bool) {
	if sessionID, ok := s.getActiveSessionID(source); ok {
		if session, exists := s.handler.chatManager.GetSession(sessionID); exists {
			if napCatSessionMatchesSourceAndPrompt(session, source, promptID) {
				s.touchActiveSession(source, sessionID)
				return session, true
			}
		}
	}

	if session, ok := s.findLatestSessionForSource(source, promptID); ok {
		s.touchActiveSession(source, session.SessionID)
		return session, true
	}

	return nil, false
}

func (s *NapCatService) createAndActivateSession(source napCatChatSource, promptID string) (*storage.ChatRecord, error) {
	session, err := s.createSessionForSource(source, promptID)
	if err != nil {
		return nil, err
	}
	s.touchActiveSession(source, session.SessionID)
	return session, nil
}

func (s *NapCatService) createSessionForSource(source napCatChatSource, promptID string) (*storage.ChatRecord, error) {
	promptID = strings.TrimSpace(promptID)
	promptName := ""
	if promptID != "" {
		if prompt, ok := s.handler.promptManager.Get(promptID); ok {
			promptName = strings.TrimSpace(prompt.Name)
		} else {
			promptID = ""
		}
	}

	for attempt := 0; attempt < 8; attempt++ {
		sessionID := generateNapCatPrivateSessionID(source.SelfID, source.UserID)
		if _, exists := s.handler.chatManager.GetSession(sessionID); exists {
			continue
		}
		return s.handler.chatManager.CreateSession(sessionID, "NapCat Chat", promptID, promptName)
	}

	return nil, fmt.Errorf("failed to allocate unique napcat session id for source %+v", source)
}

func (s *NapCatService) findLatestSessionForSource(source napCatChatSource, promptID string) (*storage.ChatRecord, bool) {
	sessions := s.listSessionsForSource(source, promptID)
	if len(sessions) == 0 {
		return nil, false
	}

	record, ok := s.handler.chatManager.GetSession(sessions[0].ID)
	return record, ok
}

func (s *NapCatService) listSessionsForSource(source napCatChatSource, promptID string) []storage.ChatSession {
	prefix := napCatPrivateSessionPrefix(source.SelfID, source.UserID)
	allSessions := s.handler.chatManager.ListSessions()

	filtered := make([]storage.ChatSession, 0, len(allSessions))
	for _, session := range allSessions {
		if !strings.HasPrefix(session.ID, prefix) {
			continue
		}
		if !napCatSessionMatchesPrompt(session.PromptID, promptID) {
			continue
		}
		filtered = append(filtered, session)
	}

	sortChatSessionsByRecent(filtered)
	return filtered
}

func sortChatSessionsByRecent(sessions []storage.ChatSession) {
	if len(sessions) <= 1 {
		return
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
				return sessions[i].ID < sessions[j].ID
			}
			return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
}

func napCatSessionMatchesSourceAndPrompt(session *storage.ChatRecord, source napCatChatSource, promptID string) bool {
	if session == nil || !source.isPrivate() {
		return false
	}
	if !strings.HasPrefix(session.SessionID, napCatPrivateSessionPrefix(source.SelfID, source.UserID)) {
		return false
	}
	return napCatSessionMatchesPrompt(session.PromptID, promptID)
}

func napCatSessionMatchesPrompt(sessionPromptID, configuredPromptID string) bool {
	return strings.TrimSpace(sessionPromptID) == strings.TrimSpace(configuredPromptID)
}

func (s *NapCatService) touchActiveSession(source napCatChatSource, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.activeSessions[source]
	if state == nil {
		state = &napCatActiveSession{}
		s.activeSessions[source] = state
	}
	state.SessionID = sessionID
	state.LastActive = time.Now()
}

func (s *NapCatService) getActiveSessionID(source napCatChatSource) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := s.activeSessions[source]
	if state == nil {
		return "", false
	}
	id := strings.TrimSpace(state.SessionID)
	return id, id != ""
}

func (s *NapCatService) cleanupLoop() {
	ticker := time.NewTicker(napCatCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupDone:
			return
		case <-ticker.C:
			s.cleanupState()
		}
	}
}

func (s *NapCatService) cleanupState() {
	now := time.Now()

	s.mu.Lock()
	for source, state := range s.activeSessions {
		if state == nil || now.Sub(state.LastActive) <= napCatConversationIdle {
			continue
		}
		delete(s.activeSessions, source)
	}
	for source, state := range s.pendingReplies {
		if state == nil || now.Sub(state.LastActive) <= napCatConversationIdle {
			continue
		}
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, source)
	}
	s.mu.Unlock()
}

func (s *NapCatService) clearAllPendingReplies() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for source, state := range s.pendingReplies {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(s.pendingReplies, source)
	}
}

func (s *NapCatService) getReplyWaitWindow() (string, time.Duration) {
	cfg := config.DefaultConfig()
	if s.handler != nil && s.handler.configManager != nil {
		cfg = s.handler.configManager.Get()
	}
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

func (s *NapCatService) enqueuePendingMessage(source napCatChatSource, message napCatPendingMessage) ([]napCatPendingMessage, bool) {
	mode, delay := s.getReplyWaitWindow()
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.pendingReplies[source]
	if state == nil {
		state = &napCatPendingReply{}
		s.pendingReplies[source] = state
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

	s.schedulePendingReplyLocked(source, state, mode, delay, now)
	return nil, true
}

func (s *NapCatService) schedulePendingReplyLocked(source napCatChatSource, state *napCatPendingReply, mode string, delay time.Duration, now time.Time) {
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
		s.flushPendingReply(nil, source)
	})
}

func (s *NapCatService) beginPendingProcessingLocked(state *napCatPendingReply) []napCatPendingMessage {
	if state == nil || state.Processing || len(state.Messages) == 0 {
		return nil
	}
	batch := append([]napCatPendingMessage(nil), state.Messages...)
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

func (s *NapCatService) flushPendingReply(ctx context.Context, source napCatChatSource) {
	s.mu.Lock()
	state := s.pendingReplies[source]
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
	s.processPendingBatchAsync(ctx, source, batch)
}

func (s *NapCatService) finishPendingReplyProcessing(ctx context.Context, source napCatChatSource) {
	mode, delay := s.getReplyWaitWindow()

	s.mu.Lock()
	state := s.pendingReplies[source]
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
		delete(s.pendingReplies, source)
	}
	s.mu.Unlock()

	if !hasMessages {
		return
	}
	if shouldFlushNow || delay <= 0 {
		s.flushPendingReply(ctx, source)
		return
	}
	if hasTimer {
		return
	}

	s.mu.Lock()
	state = s.pendingReplies[source]
	if state != nil && !state.Processing && len(state.Messages) > 0 {
		s.schedulePendingReplyLocked(source, state, mode, delay, time.Now())
	}
	s.mu.Unlock()
}

func napCatPrivateSessionPrefix(selfID, userID int64) string {
	hash := sha1.Sum([]byte(fmt.Sprintf("%d:%d", selfID, userID)))
	return "napcat_private_" + hex.EncodeToString(hash[:8]) + "_"
}

func generateNapCatPrivateSessionID(selfID, userID int64) string {
	return napCatPrivateSessionPrefix(selfID, userID) + generateID()
}

func maskNapCatSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
