package storage

import (
	"bufio"
	"bytes"
	"cornerstone/client"
	"cornerstone/logging"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ChatMessage 聊天消息
type ChatMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"` // 思考模型的推理内容
	ToolCalls        []client.ToolCall `json:"tool_calls,omitempty"`        // 工具调用
	ToolCallID       string            `json:"tool_call_id,omitempty"`      // 工具调用ID (用于tool角色消息)
	ImagePaths       []string          `json:"image_paths,omitempty"`       // 图片路径
	TTSAudioPaths    []string          `json:"tts_audio_paths,omitempty"`   // TTS音频路径
	Timestamp        time.Time         `json:"timestamp"`
}

// ChatRecord 聊天记录条目（每个会话一个JSONL文件）
type ChatRecord struct {
	ID           string        `json:"id"`
	SessionID    string        `json:"session_id"`
	Title        string        `json:"title,omitempty"`
	PromptID     string        `json:"prompt_id,omitempty"`   // 关联的 Prompt ID
	PromptName   string        `json:"prompt_name,omitempty"` // 关联的 Prompt 名称（用于文件命名）
	Messages     []ChatMessage `json:"messages"`
	Model        string        `json:"model,omitempty"`
	SystemPrompt string        `json:"system_prompt,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// ChatSession 聊天会话元数据
type ChatSession struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	PromptID   string    `json:"prompt_id,omitempty"`
	PromptName string    `json:"prompt_name,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ChatManager 聊天记录管理器
type ChatManager struct {
	dataDir      string
	mu           sync.RWMutex
	sessions     map[string]*ChatRecord
	sessionFiles map[string]string // sessionID -> 文件名（不含扩展名）
}

// NewChatManager 创建聊天管理器
func NewChatManager(dataDir string) *ChatManager {
	cm := &ChatManager{
		dataDir:      dataDir,
		sessions:     make(map[string]*ChatRecord),
		sessionFiles: make(map[string]string),
	}
	os.MkdirAll(dataDir, 0755)
	cm.loadSessions()
	return cm
}

// getSessionFilePath 获取单个会话的JSONL文件路径
func (cm *ChatManager) getSessionFilePath(sessionID string) string {
	if fileName, ok := cm.sessionFiles[sessionID]; ok {
		return filepath.Join(cm.dataDir, fileName+".jsonl")
	}
	return ""
}

// generateFileName 根据 promptName 和创建时间生成文件名
func (cm *ChatManager) generateFileName(promptName string, createdAt time.Time, sessionID string) string {
	// 清理 promptName，移除非法字符
	safeName := sanitizeFileName(promptName)
	if safeName == "" {
		safeName = "chat"
	}
	// 格式: promptName_20060102_150405_000000000_sessionID
	timestamp := createdAt.Format("20060102_150405_000000000")
	return safeName + "_" + timestamp + "_" + sessionID
}

// sanitizeFileName 清理文件名，移除非法字符
func sanitizeFileName(name string) string {
	// 移除或替换不安全的文件名字符
	re := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	safe := re.ReplaceAllString(name, "")
	// 移除首尾空格
	safe = strings.TrimSpace(safe)
	// 限制长度
	if len(safe) > 50 {
		safe = safe[:50]
	}
	return safe
}

// loadSessions 从所有JSONL文件加载会话
func (cm *ChatManager) loadSessions() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	entries, err := os.ReadDir(cm.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		logging.Errorf("sessions dir read failed: path=%s err=%v", cm.dataDir, err)
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// 只处理 .jsonl 文件
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		fileName := strings.TrimSuffix(entry.Name(), ".jsonl")
		record, err := cm.loadSessionFromFileName(fileName)
		if err != nil {
			logging.Warnf("session load failed: file=%s err=%v", fileName, err)
			continue
		}
		// 使用元数据中的 SessionID 作为 key
		cm.sessions[record.SessionID] = record
		cm.sessionFiles[record.SessionID] = fileName
	}

	return nil
}

// loadSessionFromFileName 从文件名加载会话
func (cm *ChatManager) loadSessionFromFileName(fileName string) (*ChatRecord, error) {
	filePath := filepath.Join(cm.dataDir, fileName+".jsonl")
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			logging.Warnf("session file close failed: path=%s err=%v", filePath, errClose)
		}
	}()

	record := &ChatRecord{
		Messages: []ChatMessage{},
	}

	lineNum := 0
	loggedMessageParseError := false
	reader := bufio.NewReader(file)
	for {
		rawLine, errRead := reader.ReadBytes('\n')
		if errRead != nil && errRead != io.EOF {
			return nil, errRead
		}
		if len(rawLine) > 0 {
			line := bytes.TrimSpace(rawLine)
			if len(line) > 0 {
				lineNum++

				// 第一行是会话元数据
				if lineNum == 1 {
					if errUnmarshal := json.Unmarshal(line, record); errUnmarshal != nil {
						logging.Errorf("session metadata parse failed: path=%s err=%v", filePath, errUnmarshal)
						return nil, errUnmarshal
					}
				} else {
					// 后续行是消息
					var msg ChatMessage
					if errUnmarshal := json.Unmarshal(line, &msg); errUnmarshal != nil {
						if !loggedMessageParseError {
							loggedMessageParseError = true
							logging.Warnf("session message parse failed: path=%s line=%d err=%v", filePath, lineNum, errUnmarshal)
						}
					} else {
						record.Messages = append(record.Messages, msg)
					}
				}
			}
		}

		if errRead == io.EOF {
			break
		}
	}

	// 确保 ID 和 SessionID 一致
	if record.ID == "" {
		record.ID = record.SessionID
	}
	if record.SessionID == "" {
		record.SessionID = record.ID
	}

	// UpdatedAt 优先使用最后一条消息时间，避免依赖元数据行的更新时间
	if len(record.Messages) > 0 {
		last := record.Messages[len(record.Messages)-1].Timestamp
		if record.UpdatedAt.IsZero() || last.After(record.UpdatedAt) {
			record.UpdatedAt = last
		}
	} else if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}

	return record, nil
}

func (cm *ChatManager) ensureSessionFileNameLocked(record *ChatRecord) error {
	if err := ValidateID(record.SessionID); err != nil {
		return err
	}
	if _, ok := cm.sessionFiles[record.SessionID]; ok {
		return nil
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
		record.CreatedAt = createdAt
	}
	cm.sessionFiles[record.SessionID] = cm.generateFileName(record.PromptName, createdAt, record.SessionID)
	return nil
}

func cloneChatRecord(record *ChatRecord) *ChatRecord {
	if record == nil {
		return nil
	}
	copied := *record
	copied.Messages = append([]ChatMessage(nil), record.Messages...)
	return &copied
}

func writeJSONLine(w io.Writer, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

func (cm *ChatManager) appendMessagesToFileLocked(sessionID string, messages []ChatMessage) error {
	filePath := cm.getSessionFilePath(sessionID)
	if filePath == "" {
		return os.ErrInvalid
	}

	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, msg := range messages {
		if err := writeJSONLine(file, msg); err != nil {
			return err
		}
	}
	return nil
}

func (cm *ChatManager) persistMessagesLocked(record *ChatRecord, newMessages []ChatMessage, metaChanged bool) error {
	if metaChanged {
		return cm.saveSession(record)
	}
	if err := cm.appendMessagesToFileLocked(record.SessionID, newMessages); err != nil {
		if os.IsNotExist(err) {
			return cm.saveSession(record)
		}
		return err
	}
	return nil
}

// saveSession 保存单个会话到其文件
func (cm *ChatManager) saveSession(record *ChatRecord) error {
	if err := cm.ensureSessionFileNameLocked(record); err != nil {
		logging.Errorf("session save failed: id=%s err=%v", record.SessionID, err)
		return err
	}
	filePath := cm.getSessionFilePath(record.SessionID)
	if filePath == "" {
		logging.Errorf("session save failed: id=%s err=%v", record.SessionID, os.ErrInvalid)
		return os.ErrInvalid
	}
	file, err := os.Create(filePath)
	if err != nil {
		logging.Errorf("session save failed: id=%s err=%v", record.SessionID, err)
		return err
	}
	defer file.Close()

	// 第一行写入会话元数据（不含消息）
	meta := ChatRecord{
		ID:           record.ID,
		SessionID:    record.SessionID,
		Title:        record.Title,
		PromptID:     record.PromptID,
		PromptName:   record.PromptName,
		Model:        record.Model,
		SystemPrompt: record.SystemPrompt,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
		Messages:     nil, // 元数据行不包含消息
	}
	if err := writeJSONLine(file, meta); err != nil {
		logging.Errorf("session save failed: id=%s err=%v", record.SessionID, err)
		return err
	}

	// 后续行写入每条消息
	for _, msg := range record.Messages {
		if err := writeJSONLine(file, msg); err != nil {
			logging.Errorf("session save failed: id=%s err=%v", record.SessionID, err)
			return err
		}
	}

	return nil
}

// CreateSession 创建新会话
func (cm *ChatManager) CreateSession(sessionID, title, promptID, promptName string) (*ChatRecord, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return nil, err
	}

	now := time.Now()
	record := &ChatRecord{
		ID:         sessionID,
		SessionID:  sessionID,
		Title:      title,
		PromptID:   promptID,
		PromptName: promptName,
		Messages:   []ChatMessage{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// 使用新的文件命名格式
	cm.sessionFiles[sessionID] = cm.generateFileName(promptName, now, sessionID)

	cm.sessions[sessionID] = record
	if err := cm.saveSession(record); err != nil {
		return nil, err
	}
	logging.Infof("session created: id=%s prompt=%s title=%s", record.SessionID, promptID, logging.Truncate(title, 30))
	return cloneChatRecord(record), nil
}

// GetSession 获取会话
func (cm *ChatManager) GetSession(sessionID string) (*ChatRecord, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	record, ok := cm.sessions[sessionID]
	return cloneChatRecord(record), ok
}

func cloneChatMessages(messages []ChatMessage) []ChatMessage {
	if messages == nil {
		return nil
	}

	copied := make([]ChatMessage, len(messages))
	for i := range messages {
		copied[i] = messages[i]
		if len(messages[i].ToolCalls) > 0 {
			copied[i].ToolCalls = append([]client.ToolCall(nil), messages[i].ToolCalls...)
		}
		if len(messages[i].ImagePaths) > 0 {
			copied[i].ImagePaths = append([]string(nil), messages[i].ImagePaths...)
		}
		if len(messages[i].TTSAudioPaths) > 0 {
			copied[i].TTSAudioPaths = append([]string(nil), messages[i].TTSAudioPaths...)
		}
	}

	return copied
}

func isUserTurnStart(messages []ChatMessage, index int) bool {
	if index < 0 || index >= len(messages) || messages[index].Role != "user" {
		return false
	}
	return index == 0 || messages[index-1].Role != "user"
}

// GetRecentMessages 获取最近 N 条消息（按时间顺序）
func (cm *ChatManager) GetRecentMessages(sessionID string, limit int) []ChatMessage {
	if limit <= 0 {
		return nil
	}
	if errValidateID := ValidateID(sessionID); errValidateID != nil {
		return nil
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	record, ok := cm.sessions[sessionID]
	if !ok || len(record.Messages) == 0 {
		return nil
	}

	start := 0
	if len(record.Messages) > limit {
		start = len(record.Messages) - limit
	}

	return cloneChatMessages(record.Messages[start:])
}

// GetRecentTurns 获取最近 N 轮消息（按时间顺序）。
// 一轮从一组连续的 user 消息开始，直到下一组 user 消息出现前结束。
// 如果历史中的轮数少于 N，则返回全部消息。
func (cm *ChatManager) GetRecentTurns(sessionID string, turnLimit int) []ChatMessage {
	if turnLimit <= 0 {
		return nil
	}
	if errValidateID := ValidateID(sessionID); errValidateID != nil {
		return nil
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	record, ok := cm.sessions[sessionID]
	if !ok || len(record.Messages) == 0 {
		return nil
	}

	turnCount := 0
	start := 0
	for i := len(record.Messages) - 1; i >= 0; i-- {
		if !isUserTurnStart(record.Messages, i) {
			continue
		}

		turnCount++
		start = i
		if turnCount == turnLimit {
			break
		}
	}

	if turnCount == 0 || turnCount < turnLimit {
		return cloneChatMessages(record.Messages)
	}

	return cloneChatMessages(record.Messages[start:])
}

// GetSessionMessagesPage returns a slice of messages for a session.
// If before is nil, it returns the last "limit" messages.
// If before is provided, it returns up to "limit" messages strictly before that index (0-based, exclusive).
// It also returns the starting offset (0-based) and the total message count.
func (cm *ChatManager) GetSessionMessagesPage(sessionID string, limit int, before *int) (*ChatRecord, int, int, error) {
	if err := ValidateID(sessionID); err != nil {
		return nil, 0, 0, err
	}
	if limit <= 0 {
		return nil, 0, 0, os.ErrInvalid
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	record, ok := cm.sessions[sessionID]
	if !ok {
		return nil, 0, 0, os.ErrNotExist
	}

	total := len(record.Messages)
	end := total
	if before != nil {
		if *before < 0 {
			return nil, 0, total, os.ErrInvalid
		}
		if *before < end {
			end = *before
		}
	}

	if end > total {
		end = total
	}

	start := end - limit
	if start < 0 {
		start = 0
	}

	page := *record
	page.Messages = cloneChatMessages(record.Messages[start:end])

	return &page, start, total, nil
}

// ListSessions 列出所有会话
func (cm *ChatManager) ListSessions() []ChatSession {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	sessions := make([]ChatSession, 0, len(cm.sessions))
	for _, record := range cm.sessions {
		sessions = append(sessions, ChatSession{
			ID:         record.SessionID,
			Title:      record.Title,
			PromptID:   record.PromptID,
			PromptName: record.PromptName,
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
		})
	}
	return sessions
}

// AddMessage 添加消息到会话
func (cm *ChatManager) AddMessage(sessionID, role, content string) error {
	return cm.AddMessageWithReasoning(sessionID, role, content, "")
}

// AddMessageWithReasoning 添加带思考内容的消息到会话
func (cm *ChatManager) AddMessageWithReasoning(sessionID, role, content, reasoningContent string) error {
	return cm.AddMessageWithDetails(sessionID, role, content, reasoningContent, nil, nil, nil)
}

// AddMessageWithDetails 添加带思考内容和工具调用的消息到会话
func (cm *ChatManager) AddMessageWithDetails(sessionID, role, content, reasoningContent string, imagePaths []string, toolCalls []client.ToolCall, ttsAudioPaths []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if errValidateID := ValidateID(sessionID); errValidateID != nil {
		return errValidateID
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		// 自动创建会话
		now := time.Now()
		record = &ChatRecord{
			ID:        sessionID,
			SessionID: sessionID,
			Title:     "New Chat",
			Messages:  []ChatMessage{},
			CreatedAt: now,
			UpdatedAt: now,
		}
		cm.sessions[sessionID] = record
	}
	if errEnsureSessionFileName := cm.ensureSessionFileNameLocked(record); errEnsureSessionFileName != nil {
		return errEnsureSessionFileName
	}

	now := time.Now()
	msg := ChatMessage{
		Role:             role,
		Content:          content,
		ReasoningContent: reasoningContent,
		Timestamp:        now,
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = append([]client.ToolCall(nil), toolCalls...)
	}
	if len(imagePaths) > 0 {
		msg.ImagePaths = append([]string(nil), imagePaths...)
	}
	if len(ttsAudioPaths) > 0 {
		msg.TTSAudioPaths = append([]string(nil), ttsAudioPaths...)
	}
	record.Messages = append(record.Messages, msg)
	record.UpdatedAt = now

	// 如果是第一条用户消息，用它作为标题
	metaChanged := false
	if record.Title == "New Chat" && role == "user" {
		if len(content) > 0 {
			title := content
			if len(title) > 50 {
				title = title[:50] + "..."
			}
			record.Title = title
			metaChanged = true
		} else if len(imagePaths) > 0 {
			record.Title = "图片消息"
			metaChanged = true
		}
	}

	if err := cm.persistMessagesLocked(record, []ChatMessage{msg}, metaChanged); err != nil {
		logging.Errorf("message add failed: session=%s role=%s err=%v", sessionID, role, err)
		return err
	}
	return nil
}

// AddMessages 批量添加消息
func (cm *ChatManager) AddMessages(sessionID string, messages []ChatMessage) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		now := time.Now()
		record = &ChatRecord{
			ID:        sessionID,
			SessionID: sessionID,
			Title:     "New Chat",
			Messages:  []ChatMessage{},
			CreatedAt: now,
			UpdatedAt: now,
		}
		cm.sessions[sessionID] = record
	}
	if err := cm.ensureSessionFileNameLocked(record); err != nil {
		return err
	}

	record.Messages = append(record.Messages, messages...)
	record.UpdatedAt = time.Now()

	// 设置标题
	metaChanged := false
	if record.Title == "New Chat" {
		for _, msg := range messages {
			if msg.Role != "user" {
				continue
			}
			if len(msg.Content) > 0 {
				title := msg.Content
				if len(title) > 50 {
					title = title[:50] + "..."
				}
				record.Title = title
				metaChanged = true
				break
			}
			if len(msg.ImagePaths) > 0 {
				record.Title = "图片消息"
				metaChanged = true
				break
			}
		}
	}

	return cm.persistMessagesLocked(record, messages, metaChanged)
}

// UpdateSessionTitle 更新会话标题
func (cm *ChatManager) UpdateSessionTitle(sessionID, title string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	record.Title = title
	record.UpdatedAt = time.Now()
	return cm.saveSession(record)
}

// DeleteSession 删除会话
func (cm *ChatManager) DeleteSession(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	if err := cm.ensureSessionFileNameLocked(record); err != nil {
		return err
	}

	// 删除对应的文件
	filePath := cm.getSessionFilePath(sessionID)
	if filePath != "" {
		if errRemove := os.Remove(filePath); errRemove != nil && !os.IsNotExist(errRemove) {
			logging.Errorf("session delete failed: id=%s path=%s err=%v", sessionID, filePath, errRemove)
			return errRemove
		}
	}

	delete(cm.sessions, sessionID)
	delete(cm.sessionFiles, sessionID)
	logging.Infof("session deleted: id=%s", sessionID)
	return nil
}

// ClearSession 清空会话消息
func (cm *ChatManager) ClearSession(sessionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	record.Messages = []ChatMessage{}
	record.UpdatedAt = time.Now()
	return cm.saveSession(record)
}

// UpdateMessageContentByIndex 更新指定索引的消息内容
func (cm *ChatManager) UpdateMessageContentByIndex(sessionID string, index int, content string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	if index < 0 || index >= len(record.Messages) {
		return os.ErrInvalid
	}

	record.Messages[index].Content = content
	record.UpdatedAt = time.Now()
	return cm.saveSession(record)
}

// RecallMessageByIndex 撤回指定索引的用户消息（在内容末尾追加“(已撤回)”）
func (cm *ChatManager) RecallMessageByIndex(sessionID string, index int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	if index < 0 || index >= len(record.Messages) {
		return os.ErrInvalid
	}

	msg := &record.Messages[index]
	if msg.Role != "user" {
		return os.ErrPermission
	}

	const suffix = "(已撤回)"
	trimmed := strings.TrimRight(msg.Content, " \t\r\n")
	if strings.HasSuffix(trimmed, suffix) {
		msg.Content = trimmed
	} else {
		msg.Content = trimmed + suffix
	}

	record.UpdatedAt = time.Now()
	return cm.saveSession(record)
}

// DeleteTrailingResponseBatch 删除最后一条 user 之后的整段尾部响应（用于重新生成）。
// 仅当会话末尾不是 user 时才删除；尾部是 user 时不删除任何消息。
// 返回被删除的消息条数。
func (cm *ChatManager) DeleteTrailingResponseBatch(sessionID string) (int, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return 0, err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return 0, os.ErrNotExist
	}

	n := len(record.Messages)
	if n == 0 || record.Messages[n-1].Role == "user" {
		return 0, nil
	}

	// 从尾部向前扫描，删除最后一条 user 之后的整段响应消息。
	cutIndex := n
	for cutIndex > 0 && record.Messages[cutIndex-1].Role != "user" {
		cutIndex--
	}

	deleted := n - cutIndex
	record.Messages = record.Messages[:cutIndex]
	record.UpdatedAt = time.Now()
	if err := cm.saveSession(record); err != nil {
		return 0, err
	}
	return deleted, nil
}

// DeleteTrailingAssistantBatch 兼容旧调用，语义上等价于删除最后一轮尾部响应。
func (cm *ChatManager) DeleteTrailingAssistantBatch(sessionID string) (int, error) {
	return cm.DeleteTrailingResponseBatch(sessionID)
}

// DeleteMessageByIndex 删除指定索引的消息
func (cm *ChatManager) DeleteMessageByIndex(sessionID string, index int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	if index < 0 || index >= len(record.Messages) {
		return os.ErrInvalid
	}

	record.Messages = append(record.Messages[:index], record.Messages[index+1:]...)
	record.UpdatedAt = time.Now()
	return cm.saveSession(record)
}

type redPacketReceivedArgs struct {
	PacketKey    string `json:"packet_key"`
	ReceiverName string `json:"receiver_name,omitempty"`
	SenderName   string `json:"sender_name,omitempty"`
}

func sanitizeToolCallID(raw string) string {
	if raw == "" {
		return "unknown"
	}
	var builder strings.Builder
	builder.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	sanitized := builder.String()
	if len(sanitized) > 120 {
		return sanitized[:120]
	}
	return sanitized
}

// AddRedPacketReceivedBanner 记录红包被用户打开，并追加一条“领取红包”横幅消息（幂等）
func (cm *ChatManager) AddRedPacketReceivedBanner(sessionID, packetKey, receiverName, senderName string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
	}
	if strings.TrimSpace(packetKey) == "" {
		return os.ErrInvalid
	}

	record, ok := cm.sessions[sessionID]
	if !ok {
		return os.ErrNotExist
	}

	// 幂等：如果已存在同 packetKey 的记录，不再重复写入
	for _, msg := range record.Messages {
		for _, toolCall := range msg.ToolCalls {
			if toolCall.Function.Name != "red_packet_received" {
				continue
			}
			var args redPacketReceivedArgs
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				continue
			}
			if args.PacketKey == packetKey {
				return nil
			}
		}
	}

	if err := cm.ensureSessionFileNameLocked(record); err != nil {
		return err
	}

	now := time.Now()
	arguments, errMarshal := json.Marshal(redPacketReceivedArgs{
		PacketKey:    packetKey,
		ReceiverName: receiverName,
		SenderName:   senderName,
	})
	if errMarshal != nil {
		return errMarshal
	}

	bannerMessage := ChatMessage{
		Role:      "assistant",
		Content:   "",
		Timestamp: now,
		ToolCalls: []client.ToolCall{
			{
				ID:   "red_packet_received_" + sanitizeToolCallID(packetKey),
				Type: "function",
				Function: client.ToolCallFunction{
					Name:      "red_packet_received",
					Arguments: string(arguments),
				},
			},
		},
	}

	record.Messages = append(record.Messages, bannerMessage)
	record.UpdatedAt = now
	return cm.persistMessagesLocked(record, []ChatMessage{bannerMessage}, false)
}

// GetSessionsByPromptID 根据 Prompt ID 获取所有相关的聊天会话
func (cm *ChatManager) GetSessionsByPromptID(promptID string) []ChatSession {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	sessions := make([]ChatSession, 0)
	for _, record := range cm.sessions {
		if record.PromptID == promptID {
			sessions = append(sessions, ChatSession{
				ID:         record.SessionID,
				Title:      record.Title,
				PromptID:   record.PromptID,
				PromptName: record.PromptName,
				CreatedAt:  record.CreatedAt,
				UpdatedAt:  record.UpdatedAt,
			})
		}
	}
	return sessions
}
