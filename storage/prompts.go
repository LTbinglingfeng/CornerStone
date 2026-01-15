package storage

import (
	"cornerstone/logging"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Prompt 提示词
type Prompt struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Content     string    `json:"content"`
	Description string    `json:"description,omitempty"`
	FileName    string    `json:"file_name,omitempty"` // 自定义文件名
	Avatar      string    `json:"avatar,omitempty"`    // 头像文件名
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PromptManager 提示词管理器
type PromptManager struct {
	baseDir string
	prompts map[string]Prompt
	mu      sync.RWMutex
}

// NewPromptManager 创建提示词管理器
// baseDir 是 prompts 文件夹的路径
func NewPromptManager(baseDir string) *PromptManager {
	pm := &PromptManager{
		baseDir: baseDir,
		prompts: make(map[string]Prompt),
	}
	os.MkdirAll(baseDir, 0755)
	pm.Load()
	return pm
}

// Load 从文件夹加载所有提示词
func (pm *PromptManager) Load() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.prompts = make(map[string]Prompt)

	entries, err := os.ReadDir(pm.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		promptDir := filepath.Join(pm.baseDir, entry.Name())
		prompt, err := pm.loadPromptFromDir(promptDir)
		if err != nil {
			continue // 跳过无效的prompt目录
		}
		pm.prompts[prompt.ID] = *prompt
	}

	return nil
}

// loadPromptFromDir 从目录加载单个提示词
func (pm *PromptManager) loadPromptFromDir(dir string) (*Prompt, error) {
	// 查找 prompt.json 文件
	promptFile := filepath.Join(dir, "prompt.json")
	data, err := os.ReadFile(promptFile)
	if err != nil {
		logging.Errorf("prompt load failed: path=%s err=%v", promptFile, err)
		return nil, err
	}

	var prompt Prompt
	if err := json.Unmarshal(data, &prompt); err != nil {
		logging.Errorf("prompt parse failed: path=%s err=%v", promptFile, err)
		return nil, err
	}

	return &prompt, nil
}

// getPromptDir 获取提示词的目录路径
func (pm *PromptManager) getPromptDir(id string) string {
	return filepath.Join(pm.baseDir, id)
}

// savePrompt 保存单个提示词到其目录
func (pm *PromptManager) savePrompt(prompt *Prompt) error {
	dir := pm.getPromptDir(prompt.ID)
	os.MkdirAll(dir, 0755)

	promptFile := filepath.Join(dir, "prompt.json")
	data, err := json.MarshalIndent(prompt, "", "  ")
	if err != nil {
		logging.Errorf("prompt save failed: id=%s err=%v", prompt.ID, err)
		return err
	}

	if err := os.WriteFile(promptFile, data, 0644); err != nil {
		logging.Errorf("prompt save failed: id=%s path=%s err=%v", prompt.ID, promptFile, err)
		return err
	}
	return nil
}

// Create 创建提示词
func (pm *PromptManager) Create(id, name, content, description, fileName string) (*Prompt, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := ValidateID(id); err != nil {
		return nil, err
	}

	if _, exists := pm.prompts[id]; exists {
		return nil, fmt.Errorf("prompt with id %s already exists", id)
	}

	now := time.Now()
	prompt := Prompt{
		ID:          id,
		Name:        name,
		Content:     content,
		Description: description,
		FileName:    fileName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := pm.savePrompt(&prompt); err != nil {
		return nil, err
	}

	pm.prompts[id] = prompt
	logging.Infof("prompt created: id=%s name=%s", prompt.ID, prompt.Name)
	return &prompt, nil
}

// Get 获取单个提示词
func (pm *PromptManager) Get(id string) (*Prompt, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	prompt, ok := pm.prompts[id]
	if !ok {
		return nil, false
	}
	return &prompt, true
}

// List 列出所有提示词
func (pm *PromptManager) List() []Prompt {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	prompts := make([]Prompt, 0, len(pm.prompts))
	for _, p := range pm.prompts {
		prompts = append(prompts, p)
	}
	return prompts
}

// Update 更新提示词
func (pm *PromptManager) Update(id, name, content, description, fileName string) (*Prompt, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := ValidateID(id); err != nil {
		return nil, err
	}

	prompt, ok := pm.prompts[id]
	if !ok {
		return nil, os.ErrNotExist
	}

	if name != "" {
		prompt.Name = name
	}
	if content != "" {
		prompt.Content = content
	}
	if description != "" {
		prompt.Description = description
	}
	if fileName != "" {
		prompt.FileName = fileName
	}
	prompt.UpdatedAt = time.Now()

	if err := pm.savePrompt(&prompt); err != nil {
		return nil, err
	}

	pm.prompts[id] = prompt
	logging.Infof("prompt updated: id=%s name=%s", prompt.ID, prompt.Name)
	return &prompt, nil
}

// Delete 删除提示词
func (pm *PromptManager) Delete(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := ValidateID(id); err != nil {
		return err
	}

	if _, ok := pm.prompts[id]; !ok {
		return os.ErrNotExist
	}

	// 删除整个目录
	dir := pm.getPromptDir(id)
	if err := os.RemoveAll(dir); err != nil {
		logging.Errorf("prompt delete failed: id=%s err=%v", id, err)
		return err
	}

	delete(pm.prompts, id)
	logging.Infof("prompt deleted: id=%s", id)
	return nil
}

// SaveAvatar 保存头像文件
func (pm *PromptManager) SaveAvatar(id string, filename string, data io.Reader) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := ValidateID(id); err != nil {
		return "", err
	}
	if err := ValidateFileName(filename); err != nil {
		return "", err
	}

	prompt, ok := pm.prompts[id]
	if !ok {
		return "", os.ErrNotExist
	}

	dir := pm.getPromptDir(id)
	os.MkdirAll(dir, 0755)

	// 删除旧的头像文件
	if prompt.Avatar != "" {
		if err := ValidateFileName(prompt.Avatar); err == nil {
			oldAvatarPath := filepath.Join(dir, prompt.Avatar)
			os.Remove(oldAvatarPath)
		}
	}

	// 保存新头像
	avatarPath := filepath.Join(dir, filename)
	file, err := os.Create(avatarPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, data); err != nil {
		return "", err
	}

	// 更新prompt的头像字段
	prompt.Avatar = filename
	prompt.UpdatedAt = time.Now()

	if err := pm.savePrompt(&prompt); err != nil {
		return "", err
	}

	pm.prompts[id] = prompt
	return filename, nil
}

// GetAvatarPath 获取头像文件路径
func (pm *PromptManager) GetAvatarPath(id string) (string, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if err := ValidateID(id); err != nil {
		return "", err
	}

	prompt, ok := pm.prompts[id]
	if !ok {
		return "", os.ErrNotExist
	}

	if prompt.Avatar == "" {
		return "", os.ErrNotExist
	}

	if err := ValidateFileName(prompt.Avatar); err != nil {
		return "", err
	}

	avatarPath := filepath.Join(pm.getPromptDir(id), prompt.Avatar)
	if _, err := os.Stat(avatarPath); err != nil {
		return "", err
	}

	return avatarPath, nil
}

// SetAvatar 设置头像文件名（用于创建时直接设置）
func (pm *PromptManager) SetAvatar(id, avatar string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := ValidateID(id); err != nil {
		return err
	}
	if avatar != "" {
		if err := ValidateFileName(avatar); err != nil {
			return err
		}
	}

	prompt, ok := pm.prompts[id]
	if !ok {
		return os.ErrNotExist
	}

	prompt.Avatar = avatar
	prompt.UpdatedAt = time.Now()

	if err := pm.savePrompt(&prompt); err != nil {
		return err
	}

	pm.prompts[id] = prompt
	return nil
}

// GetBaseDir 获取基础目录
func (pm *PromptManager) GetBaseDir() string {
	return pm.baseDir
}
