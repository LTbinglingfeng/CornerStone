package storage

import (
	"bufio"
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
	Role             string    `json:"role"`
	Content          string    `json:"content"`
	ReasoningContent string    `json:"reasoning_content,omitempty"` // 思考模型的推理内容
	Timestamp        time.Time `json:"timestamp"`
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
	defer file.Close()

	record := &ChatRecord{
		Messages: []ChatMessage{},
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// 第一行是会话元数据
		if lineNum == 1 {
			if err := json.Unmarshal(line, record); err != nil {
				continue
			}
		} else {
			// 后续行是消息
			var msg ChatMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			record.Messages = append(record.Messages, msg)
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

	return record, scanner.Err()
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
		return err
	}
	filePath := cm.getSessionFilePath(record.SessionID)
	if filePath == "" {
		return os.ErrInvalid
	}
	file, err := os.Create(filePath)
	if err != nil {
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
		return err
	}

	// 后续行写入每条消息
	for _, msg := range record.Messages {
		if err := writeJSONLine(file, msg); err != nil {
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
	return cloneChatRecord(record), nil
}

// GetSession 获取会话
func (cm *ChatManager) GetSession(sessionID string) (*ChatRecord, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	record, ok := cm.sessions[sessionID]
	return cloneChatRecord(record), ok
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
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if err := ValidateID(sessionID); err != nil {
		return err
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
	if err := cm.ensureSessionFileNameLocked(record); err != nil {
		return err
	}

	now := time.Now()
	msg := ChatMessage{
		Role:             role,
		Content:          content,
		ReasoningContent: reasoningContent,
		Timestamp:        now,
	}
	record.Messages = append(record.Messages, msg)
	record.UpdatedAt = now

	// 如果是第一条用户消息，用它作为标题
	metaChanged := false
	if record.Title == "New Chat" && role == "user" && len(content) > 0 {
		title := content
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		record.Title = title
		metaChanged = true
	}

	return cm.persistMessagesLocked(record, []ChatMessage{msg}, metaChanged)
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
			if msg.Role == "user" && len(msg.Content) > 0 {
				title := msg.Content
				if len(title) > 50 {
					title = title[:50] + "..."
				}
				record.Title = title
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
		os.Remove(filePath)
	}

	delete(cm.sessions, sessionID)
	delete(cm.sessionFiles, sessionID)
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
