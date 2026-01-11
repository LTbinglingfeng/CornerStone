package config

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// 错误定义
var (
	ErrProviderExists           = errors.New("provider already exists")
	ErrProviderNotFound         = errors.New("provider not found")
	ErrCannotDeleteLastProvider = errors.New("cannot delete the last provider")
)

// ProviderType 供应商类型
type ProviderType string

const (
	ProviderTypeOpenAI    ProviderType = "openai"    // OpenAI兼容API
	ProviderTypeGemini    ProviderType = "gemini"    // Google Gemini API
	ProviderTypeAnthropic ProviderType = "anthropic" // Anthropic Claude API
)

const (
	DefaultProviderTemperature     = 0.8
	DefaultProviderTopP            = 1.0
	DefaultProviderContextMessages = 64
)

// Provider 供应商配置
type Provider struct {
	ID              string       `json:"id"`               // 供应商唯一标识
	Name            string       `json:"name"`             // 显示名称
	Type            ProviderType `json:"type"`             // 供应商类型 (openai/gemini/anthropic)
	BaseURL         string       `json:"base_url"`         // API基础URL
	APIKey          string       `json:"api_key"`          // API密钥
	Model           string       `json:"model"`            // 默认模型
	Temperature     float64      `json:"temperature"`      // 温度
	TopP            float64      `json:"top_p"`            // Top P
	ContextMessages int          `json:"context_messages"` // 上下文消息轮数
	Stream          bool         `json:"stream"`           // 是否启用流式输出
	ImageCapable    bool         `json:"image_capable"`    // 是否支持识图
}

// Config 存储应用配置信息
type Config struct {
	Providers        []Provider `json:"providers"`          // 供应商列表
	ActiveProviderID string     `json:"active_provider_id"` // 当前激活的供应商ID
	SystemPrompt     string     `json:"system_prompt"`      // 全局系统提示词
}

// Manager 配置管理器
type Manager struct {
	config     Config
	configPath string
	mu         sync.RWMutex
}

// DefaultProvider 返回默认供应商
func DefaultProvider() Provider {
	return Provider{
		ID:              "default",
		Name:            "OpenAI",
		Type:            ProviderTypeOpenAI,
		BaseURL:         "https://api.openai.com/v1",
		APIKey:          "",
		Model:           "gpt-3.5-turbo",
		Temperature:     DefaultProviderTemperature,
		TopP:            DefaultProviderTopP,
		ContextMessages: DefaultProviderContextMessages,
		Stream:          true,
		ImageCapable:    false,
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Providers:        []Provider{DefaultProvider()},
		ActiveProviderID: "default",
		SystemPrompt:     "You are a helpful assistant.",
	}
}

// NewManager 创建新的配置管理器
func NewManager(configPath string) *Manager {
	m := &Manager{
		config:     DefaultConfig(),
		configPath: configPath,
	}
	m.Load()
	return m
}

// Load 从文件加载配置
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.saveUnsafe()
		}
		return err
	}

	// 尝试解析新格式
	if err := json.Unmarshal(data, &m.config); err != nil {
		return err
	}

	var rawProviders struct {
		Providers []map[string]json.RawMessage `json:"providers"`
	}
	_ = json.Unmarshal(data, &rawProviders)

	// 检查是否是旧格式配置（没有providers字段）
	// 如果 Providers 为空，尝试从旧格式迁移
	if len(m.config.Providers) == 0 {
		var oldConfig struct {
			BaseURL      string `json:"base_url"`
			APIKey       string `json:"api_key"`
			Model        string `json:"model"`
			SystemPrompt string `json:"system_prompt"`
		}
		if err := json.Unmarshal(data, &oldConfig); err == nil && oldConfig.BaseURL != "" {
			// 迁移旧配置到新格式
			m.config = Config{
				Providers: []Provider{
					{
						ID:              "default",
						Name:            "Default Provider",
						Type:            ProviderTypeOpenAI, // 默认使用OpenAI类型
						BaseURL:         oldConfig.BaseURL,
						APIKey:          oldConfig.APIKey,
						Model:           oldConfig.Model,
						Temperature:     DefaultProviderTemperature,
						TopP:            DefaultProviderTopP,
						ContextMessages: DefaultProviderContextMessages,
					},
				},
				ActiveProviderID: "default",
				SystemPrompt:     oldConfig.SystemPrompt,
			}
			// 保存新格式
			return m.saveUnsafe()
		}
	}

	if m.applyProviderDefaults(rawProviders.Providers) {
		return m.saveUnsafe()
	}

	return nil
}

func (m *Manager) applyProviderDefaults(rawProviders []map[string]json.RawMessage) bool {
	changed := false
	for i := range m.config.Providers {
		provider := &m.config.Providers[i]
		var raw map[string]json.RawMessage
		if i < len(rawProviders) {
			raw = rawProviders[i]
		}
		if raw == nil || raw["temperature"] == nil {
			if provider.Temperature == 0 {
				provider.Temperature = DefaultProviderTemperature
				changed = true
			}
		}
		if raw == nil || raw["top_p"] == nil {
			if provider.TopP == 0 {
				provider.TopP = DefaultProviderTopP
				changed = true
			}
		}
		if raw == nil || raw["context_messages"] == nil {
			if provider.ContextMessages == 0 {
				provider.ContextMessages = DefaultProviderContextMessages
				changed = true
			}
		}
	}
	return changed
}

// Save 保存配置到文件
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveUnsafe()
}

func (m *Manager) saveUnsafe() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		return err
	}
	_ = os.Chmod(m.configPath, 0600)
	return nil
}

// Get 获取当前配置
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Update 更新配置
func (m *Manager) Update(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	return m.saveUnsafe()
}

// UpdatePartial 部分更新配置（兼容旧API，更新当前激活供应商）
func (m *Manager) UpdatePartial(updates map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 更新系统提示词
	if v, ok := updates["system_prompt"]; ok {
		m.config.SystemPrompt = v
	}

	// 更新当前激活供应商的配置
	for i := range m.config.Providers {
		if m.config.Providers[i].ID == m.config.ActiveProviderID {
			if v, ok := updates["base_url"]; ok {
				m.config.Providers[i].BaseURL = v
			}
			if v, ok := updates["api_key"]; ok {
				m.config.Providers[i].APIKey = v
			}
			if v, ok := updates["model"]; ok {
				m.config.Providers[i].Model = v
			}
			break
		}
	}

	return m.saveUnsafe()
}

// GetActiveProvider 获取当前激活的供应商配置
func (m *Manager) GetActiveProvider() *Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.config.Providers {
		if m.config.Providers[i].ID == m.config.ActiveProviderID {
			provider := m.config.Providers[i]
			return &provider
		}
	}
	// 如果没有找到激活供应商，返回第一个
	if len(m.config.Providers) > 0 {
		provider := m.config.Providers[0]
		return &provider
	}
	return nil
}

// GetProviders 获取所有供应商列表
func (m *Manager) GetProviders() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	providers := make([]Provider, len(m.config.Providers))
	copy(providers, m.config.Providers)
	return providers
}

// GetProvider 根据ID获取供应商
func (m *Manager) GetProvider(id string) *Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.config.Providers {
		if m.config.Providers[i].ID == id {
			provider := m.config.Providers[i]
			return &provider
		}
	}
	return nil
}

// AddProvider 添加新供应商
func (m *Manager) AddProvider(provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查ID是否重复
	for _, p := range m.config.Providers {
		if p.ID == provider.ID {
			return ErrProviderExists
		}
	}

	m.config.Providers = append(m.config.Providers, provider)
	return m.saveUnsafe()
}

// UpdateProvider 更新供应商配置
func (m *Manager) UpdateProvider(provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Providers {
		if m.config.Providers[i].ID == provider.ID {
			m.config.Providers[i] = provider
			return m.saveUnsafe()
		}
	}
	return ErrProviderNotFound
}

// DeleteProvider 删除供应商
func (m *Manager) DeleteProvider(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 不能删除最后一个供应商
	if len(m.config.Providers) <= 1 {
		return ErrCannotDeleteLastProvider
	}

	for i := range m.config.Providers {
		if m.config.Providers[i].ID == id {
			m.config.Providers = append(m.config.Providers[:i], m.config.Providers[i+1:]...)
			// 如果删除的是当前激活供应商，切换到第一个
			if m.config.ActiveProviderID == id && len(m.config.Providers) > 0 {
				m.config.ActiveProviderID = m.config.Providers[0].ID
			}
			return m.saveUnsafe()
		}
	}
	return ErrProviderNotFound
}

// SetActiveProvider 设置激活的供应商
func (m *Manager) SetActiveProvider(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.config.Providers {
		if p.ID == id {
			m.config.ActiveProviderID = id
			return m.saveUnsafe()
		}
	}
	return ErrProviderNotFound
}

// UpdateSystemPrompt 更新系统提示词
func (m *Manager) UpdateSystemPrompt(prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.SystemPrompt = prompt
	return m.saveUnsafe()
}

// GetSystemPrompt 获取系统提示词
func (m *Manager) GetSystemPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.SystemPrompt
}

// GetActiveProviderID 获取激活的供应商ID
func (m *Manager) GetActiveProviderID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.ActiveProviderID
}
