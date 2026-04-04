package config

import (
	"bytes"
	"cornerstone/logging"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// 错误定义
var (
	ErrProviderExists             = errors.New("provider already exists")
	ErrProviderNotFound           = errors.New("provider not found")
	ErrCannotDeleteLastProvider   = errors.New("cannot delete the last provider")
	ErrProviderNotChatCapable     = errors.New("provider is not chat-capable")
	ErrProviderNotImageGenCapable = errors.New("provider is not image-generation capable")
)

// ProviderType 供应商类型
type ProviderType string

const (
	ProviderTypeOpenAI         ProviderType = "openai"          // OpenAI兼容API（Chat Completions）
	ProviderTypeOpenAIResponse ProviderType = "openai_response" // OpenAI Responses API
	ProviderTypeGemini         ProviderType = "gemini"          // Google Gemini API
	ProviderTypeGeminiImage    ProviderType = "gemini_image"    // Google Gemini 生图（Imagen）
	ProviderTypeAnthropic      ProviderType = "anthropic"       // Anthropic Claude API
)

type TTSProviderType string

const (
	TTSProviderTypeMinimax TTSProviderType = "minimax"
)

type TTSVoiceSetting struct {
	VoiceID string  `json:"voice_id"`
	Speed   float64 `json:"speed"`
}

type TTSProvider struct {
	Type          TTSProviderType `json:"type"`
	BaseURL       string          `json:"base_url"`
	APIKey        string          `json:"api_key"`
	Model         string          `json:"model"`
	VoiceSetting  TTSVoiceSetting `json:"voice_setting"`
	LanguageBoost string          `json:"language_boost,omitempty"`
}

type WeatherCity struct {
	Name        string `json:"name"`
	Affiliation string `json:"affiliation"`
	LocationKey string `json:"location_key"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

type ClawBotConfig struct {
	Enabled            bool            `json:"enabled"`
	BaseURL            string          `json:"base_url"`
	BotToken           string          `json:"bot_token,omitempty"`
	ILinkUserID        string          `json:"ilink_user_id,omitempty"`
	PromptID           string          `json:"prompt_id,omitempty"`
	GetUpdatesBuf      string          `json:"get_updates_buf,omitempty"`
	CommandPermissions map[string]bool `json:"command_permissions,omitempty"`
}

type ReplyWaitWindowMode string

const (
	ReplyWaitWindowModeFixed   ReplyWaitWindowMode = "fixed"
	ReplyWaitWindowModeSliding ReplyWaitWindowMode = "sliding"
)

func isChatProviderType(providerType ProviderType) bool {
	return providerType != ProviderTypeGeminiImage
}

func isImageGenProviderType(providerType ProviderType) bool {
	return providerType == ProviderTypeGeminiImage
}

const (
	DefaultProviderTemperature     = 0.8
	DefaultProviderTopP            = 1.0
	DefaultProviderContextMessages = 64
	DefaultMemoryExtractionRounds  = 5
	DefaultMemoryRefreshInterval   = 5
	MaxMemoryRefreshInterval       = 99
	DefaultClawBotBaseURL          = "https://ilinkai.weixin.qq.com"
	DefaultReplyWaitWindowSeconds  = 2
	MaxReplyWaitWindowSeconds      = 120
	DefaultTimeZone                = "Asia/Shanghai"
)

var defaultClawBotCommandPermissionKeys = []string{
	"new",
	"ls",
	"checkout",
	"rename",
	"delete",
	"prompt",
	"re",
}

func DefaultClawBotCommandPermissions() map[string]bool {
	permissions := make(map[string]bool, len(defaultClawBotCommandPermissionKeys))
	for _, key := range defaultClawBotCommandPermissionKeys {
		permissions[key] = true
	}
	return permissions
}

func NormalizeClawBotCommandPermissions(permissions map[string]bool) map[string]bool {
	normalized := DefaultClawBotCommandPermissions()
	for _, key := range defaultClawBotCommandPermissionKeys {
		if value, ok := permissions[key]; ok {
			normalized[key] = value
		}
	}
	return normalized
}

func clawBotCommandPermissionsEqual(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		otherValue, ok := right[key]
		if !ok || otherValue != value {
			return false
		}
	}
	return true
}

func cloneWeatherCity(city *WeatherCity) *WeatherCity {
	if city == nil {
		return nil
	}
	clone := *city
	return &clone
}

func weatherCitiesEqual(left, right *WeatherCity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Name == right.Name &&
		left.Affiliation == right.Affiliation &&
		left.LocationKey == right.LocationKey &&
		left.Latitude == right.Latitude &&
		left.Longitude == right.Longitude
}

func normalizeWeatherCity(city *WeatherCity) *WeatherCity {
	if city == nil {
		return nil
	}
	normalized := &WeatherCity{
		Name:        strings.TrimSpace(city.Name),
		Affiliation: strings.TrimSpace(city.Affiliation),
		LocationKey: strings.TrimSpace(city.LocationKey),
		Latitude:    strings.TrimSpace(city.Latitude),
		Longitude:   strings.TrimSpace(city.Longitude),
	}
	if normalized.Name == "" &&
		normalized.Affiliation == "" &&
		normalized.LocationKey == "" &&
		normalized.Latitude == "" &&
		normalized.Longitude == "" {
		return nil
	}
	if normalized.Name == "" || normalized.LocationKey == "" || normalized.Latitude == "" || normalized.Longitude == "" {
		return nil
	}
	return normalized
}

// Provider 供应商配置
type Provider struct {
	ID                        string       `json:"id"`                                      // 供应商唯一标识
	Name                      string       `json:"name"`                                    // 显示名称
	Type                      ProviderType `json:"type"`                                    // 供应商类型 (openai/openai_response/gemini/gemini_image/anthropic)
	BaseURL                   string       `json:"base_url"`                                // API基础URL
	APIKey                    string       `json:"api_key"`                                 // API密钥
	Model                     string       `json:"model"`                                   // 默认模型
	Temperature               float64      `json:"temperature"`                             // 温度
	TopP                      float64      `json:"top_p"`                                   // Top P
	ThinkingBudget            int          `json:"thinking_budget"`                         // 思考预算（Anthropic）
	PromptCaching             bool         `json:"prompt_caching"`                          // 是否启用 Anthropic Prompt Caching
	PromptCacheTTL            string       `json:"prompt_cache_ttl,omitempty"`              // Anthropic Prompt Caching TTL (5m/1h)
	ReasoningEffort           string       `json:"reasoning_effort"`                        // 思考强度（OpenAI兼容）
	GeminiThinkingMode        *string      `json:"gemini_thinking_mode,omitempty"`          // Gemini思考模式 (none/thinking_level/thinking_budget)
	GeminiThinkingLevel       *string      `json:"gemini_thinking_level,omitempty"`         // Gemini思考级别 (model-dependent, e.g. low/high/minimal/medium)
	GeminiThinkingBudget      *int         `json:"gemini_thinking_budget,omitempty"`        // Gemini思考预算 (model-dependent, e.g. 0-24576 or 128-32768)
	GeminiImageAspectRatio    *string      `json:"gemini_image_aspect_ratio,omitempty"`     // Gemini生图比例 (1:1/3:4/4:3/9:16/16:9)
	GeminiImageSize           *string      `json:"gemini_image_size,omitempty"`             // Gemini生图分辨率 (最大边: 1K/2K，留空使用默认)
	GeminiImageNumberOfImages *int         `json:"gemini_image_number_of_images,omitempty"` // Gemini生图张数 (1-8)
	GeminiImageOutputMIMEType *string      `json:"gemini_image_output_mime_type,omitempty"` // Gemini生图输出格式 (image/jpeg/image/png)
	ContextMessages           int          `json:"context_messages"`                        // 上下文轮数
	Stream                    bool         `json:"stream"`                                  // 是否启用流式输出
	ImageCapable              bool         `json:"image_capable"`                           // 是否支持识图
}

// Config 存储应用配置信息
type Config struct {
	Providers              []Provider    `json:"providers"`                 // 供应商列表
	ActiveProviderID       string        `json:"active_provider_id"`        // 当前激活的供应商ID
	ImageProviderID        string        `json:"image_provider_id"`         // 生图供应商ID（gemini_image）
	MemoryProviderID       string        `json:"memory_provider_id"`        // 记忆提取模型（供应商ID）
	MemoryProvider         *Provider     `json:"memory_provider"`           // 记忆提取模型（独立配置）
	MemoryEnabled          bool          `json:"memory_enabled"`            // 记忆功能开关
	MemoryExtractionRounds int           `json:"memory_extraction_rounds"`  // 记忆提取上传的对话轮数（每轮从用户发言开始，直到下一轮用户发言前结束）
	MemoryRefreshInterval  int           `json:"memory_refresh_interval"`   // 记忆刷新间隔（按对话轮数）
	TTSEnabled             bool          `json:"tts_enabled"`               // TTS开关
	TTSProvider            *TTSProvider  `json:"tts_provider,omitempty"`    // TTS提供商（仅支持 MiniMax）
	SystemPrompt           string        `json:"system_prompt"`             // 全局系统提示词
	ReplyWaitWindowMode    string        `json:"reply_wait_window_mode"`    // 回复等候窗口模式 (fixed/sliding)
	ReplyWaitWindowSeconds int           `json:"reply_wait_window_seconds"` // 回复等候窗口秒数
	TimeZone               string        `json:"time_zone"`                 // Agent 时间工具使用的时区
	WeatherDefaultCity     *WeatherCity  `json:"weather_default_city,omitempty"`
	TLSCertPath            string        `json:"tls_cert_path,omitempty"` // TLS证书路径(PEM)，留空禁用HTTPS
	TLSKeyPath             string        `json:"tls_key_path,omitempty"`  // TLS私钥路径(PEM)，留空禁用HTTPS
	ClawBot                ClawBotConfig `json:"clawbot"`                 // 微信 iLink ClawBot 配置
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
		ThinkingBudget:  0,
		PromptCaching:   false,
		PromptCacheTTL:  "5m",
		ReasoningEffort: "",
		ContextMessages: DefaultProviderContextMessages,
		Stream:          true,
		ImageCapable:    false,
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Providers:              []Provider{DefaultProvider()},
		ActiveProviderID:       "default",
		ImageProviderID:        "",
		MemoryProviderID:       "",
		MemoryProvider:         nil,
		MemoryEnabled:          false,
		MemoryExtractionRounds: DefaultMemoryExtractionRounds,
		MemoryRefreshInterval:  DefaultMemoryRefreshInterval,
		TTSEnabled:             false,
		TTSProvider:            nil,
		SystemPrompt:           "You are a helpful assistant.",
		ReplyWaitWindowMode:    string(ReplyWaitWindowModeSliding),
		ReplyWaitWindowSeconds: DefaultReplyWaitWindowSeconds,
		TimeZone:               DefaultTimeZone,
		WeatherDefaultCity:     nil,
		ClawBot: ClawBotConfig{
			BaseURL:            DefaultClawBotBaseURL,
			CommandPermissions: DefaultClawBotCommandPermissions(),
		},
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
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			return nil
		}
		logging.Errorf("config load failed: path=%s err=%v", m.configPath, err)
		return err
	}

	// 兼容 Windows 编辑器写入的 UTF-8 BOM（EF BB BF）
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	// 尝试解析新格式
	if err := json.Unmarshal(data, &m.config); err != nil {
		logging.Errorf("config parse failed: path=%s err=%v", m.configPath, err)
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
						PromptCaching:   false,
						PromptCacheTTL:  "5m",
						ContextMessages: DefaultProviderContextMessages,
					},
				},
				ActiveProviderID:       "default",
				MemoryProviderID:       "",
				MemoryProvider:         nil,
				MemoryEnabled:          false,
				MemoryExtractionRounds: DefaultMemoryExtractionRounds,
				MemoryRefreshInterval:  DefaultMemoryRefreshInterval,
				SystemPrompt:           oldConfig.SystemPrompt,
			}
			m.applyConfigDefaults()
			// 保存新格式
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			return nil
		}
	}

	if m.applyProviderDefaults(rawProviders.Providers) {
		changed := true
		if m.applyConfigDefaults() {
			changed = true
		}
		if changed {
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
		}
	} else if m.applyConfigDefaults() {
		if errSave := m.saveUnsafe(); errSave != nil {
			return errSave
		}
	}
	return nil
}

func (m *Manager) applyConfigDefaults() bool {
	changed := false
	replyWaitMode := normalizeReplyWaitWindowMode(m.config.ReplyWaitWindowMode)
	if replyWaitMode != m.config.ReplyWaitWindowMode {
		m.config.ReplyWaitWindowMode = replyWaitMode
		changed = true
	}
	replyWaitSeconds := normalizeReplyWaitWindowSeconds(m.config.ReplyWaitWindowSeconds)
	if replyWaitSeconds != m.config.ReplyWaitWindowSeconds {
		m.config.ReplyWaitWindowSeconds = replyWaitSeconds
		changed = true
	}
	timeZone := normalizeTimeZone(m.config.TimeZone)
	if timeZone != m.config.TimeZone {
		m.config.TimeZone = timeZone
		changed = true
	}
	normalizedWeatherCity := normalizeWeatherCity(m.config.WeatherDefaultCity)
	if !weatherCitiesEqual(m.config.WeatherDefaultCity, normalizedWeatherCity) {
		m.config.WeatherDefaultCity = normalizedWeatherCity
		changed = true
	}
	if m.config.MemoryExtractionRounds <= 0 {
		m.config.MemoryExtractionRounds = DefaultMemoryExtractionRounds
		changed = true
	}
	if m.config.MemoryRefreshInterval <= 0 {
		m.config.MemoryRefreshInterval = DefaultMemoryRefreshInterval
		changed = true
	}
	imageProviderID := strings.TrimSpace(m.config.ImageProviderID)
	if imageProviderID != m.config.ImageProviderID {
		m.config.ImageProviderID = imageProviderID
		changed = true
	}
	if imageProviderID != "" {
		valid := false
		for i := range m.config.Providers {
			if m.config.Providers[i].ID == imageProviderID && isImageGenProviderType(m.config.Providers[i].Type) {
				valid = true
				break
			}
		}
		if !valid {
			m.config.ImageProviderID = ""
			changed = true
		}
	}
	clawBotBaseURL := strings.TrimSpace(m.config.ClawBot.BaseURL)
	if clawBotBaseURL == "" {
		clawBotBaseURL = DefaultClawBotBaseURL
	}
	if clawBotBaseURL != m.config.ClawBot.BaseURL {
		m.config.ClawBot.BaseURL = clawBotBaseURL
		changed = true
	}
	promptID := strings.TrimSpace(m.config.ClawBot.PromptID)
	if promptID != m.config.ClawBot.PromptID {
		m.config.ClawBot.PromptID = promptID
		changed = true
	}
	ilinkUserID := strings.TrimSpace(m.config.ClawBot.ILinkUserID)
	if ilinkUserID != m.config.ClawBot.ILinkUserID {
		m.config.ClawBot.ILinkUserID = ilinkUserID
		changed = true
	}
	botToken := strings.TrimSpace(m.config.ClawBot.BotToken)
	if botToken != m.config.ClawBot.BotToken {
		m.config.ClawBot.BotToken = botToken
		changed = true
	}
	getUpdatesBuf := strings.TrimSpace(m.config.ClawBot.GetUpdatesBuf)
	if getUpdatesBuf != m.config.ClawBot.GetUpdatesBuf {
		m.config.ClawBot.GetUpdatesBuf = getUpdatesBuf
		changed = true
	}
	normalizedCommandPermissions := NormalizeClawBotCommandPermissions(m.config.ClawBot.CommandPermissions)
	if !clawBotCommandPermissionsEqual(m.config.ClawBot.CommandPermissions, normalizedCommandPermissions) {
		m.config.ClawBot.CommandPermissions = normalizedCommandPermissions
		changed = true
	}
	return changed
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
		if provider.Type == ProviderTypeAnthropic || provider.PromptCaching || (raw != nil && raw["prompt_cache_ttl"] != nil) {
			promptCacheTTL := normalizeAnthropicPromptCacheTTL(provider.PromptCacheTTL)
			if provider.PromptCacheTTL != promptCacheTTL {
				provider.PromptCacheTTL = promptCacheTTL
				changed = true
			}
		}

		if provider.Type != ProviderTypeGemini {
			if provider.GeminiThinkingMode != nil {
				provider.GeminiThinkingMode = nil
				changed = true
			}
			if provider.GeminiThinkingLevel != nil {
				provider.GeminiThinkingLevel = nil
				changed = true
			}
			if provider.GeminiThinkingBudget != nil {
				provider.GeminiThinkingBudget = nil
				changed = true
			}
		} else {
			mode := "none"
			if provider.GeminiThinkingMode != nil {
				mode = strings.TrimSpace(*provider.GeminiThinkingMode)
			}
			if mode == "" {
				mode = "none"
			}
			if mode != "none" && mode != "thinking_level" && mode != "thinking_budget" {
				mode = "none"
			}
			if provider.GeminiThinkingMode == nil || *provider.GeminiThinkingMode != mode {
				provider.GeminiThinkingMode = &mode
				changed = true
			}

			level := "low"
			if provider.GeminiThinkingLevel != nil {
				level = strings.TrimSpace(*provider.GeminiThinkingLevel)
			}
			if level == "" {
				level = "low"
			}
			level = normalizeGeminiThinkingLevel(provider.Model, level)
			if provider.GeminiThinkingLevel == nil || *provider.GeminiThinkingLevel != level {
				provider.GeminiThinkingLevel = &level
				changed = true
			}

			minBudget, maxBudget := geminiThinkingBudgetRange(provider.Model)
			budget := minBudget
			if provider.GeminiThinkingBudget != nil {
				budget = *provider.GeminiThinkingBudget
			}
			if budget != -1 {
				if budget < minBudget {
					budget = minBudget
				}
				if budget > maxBudget {
					budget = maxBudget
				}
			}
			if provider.GeminiThinkingBudget == nil || *provider.GeminiThinkingBudget != budget {
				provider.GeminiThinkingBudget = &budget
				changed = true
			}
		}

		if provider.Type != ProviderTypeGeminiImage {
			if provider.GeminiImageAspectRatio != nil {
				provider.GeminiImageAspectRatio = nil
				changed = true
			}
			if provider.GeminiImageSize != nil {
				provider.GeminiImageSize = nil
				changed = true
			}
			if provider.GeminiImageNumberOfImages != nil {
				provider.GeminiImageNumberOfImages = nil
				changed = true
			}
			if provider.GeminiImageOutputMIMEType != nil {
				provider.GeminiImageOutputMIMEType = nil
				changed = true
			}
		} else {
			aspectRatio := "1:1"
			if provider.GeminiImageAspectRatio != nil {
				aspectRatio = strings.TrimSpace(*provider.GeminiImageAspectRatio)
			}
			aspectRatio = normalizeGeminiImageAspectRatio(aspectRatio)
			if provider.GeminiImageAspectRatio == nil || *provider.GeminiImageAspectRatio != aspectRatio {
				provider.GeminiImageAspectRatio = &aspectRatio
				changed = true
			}

			size := ""
			if provider.GeminiImageSize != nil {
				size = strings.TrimSpace(*provider.GeminiImageSize)
			}
			size = normalizeGeminiImageSize(size)
			if size == "" {
				if provider.GeminiImageSize != nil {
					provider.GeminiImageSize = nil
					changed = true
				}
			} else if provider.GeminiImageSize == nil || *provider.GeminiImageSize != size {
				provider.GeminiImageSize = &size
				changed = true
			}

			numberOfImages := 1
			if provider.GeminiImageNumberOfImages != nil {
				numberOfImages = *provider.GeminiImageNumberOfImages
			}
			numberOfImages = clampGeminiImageNumberOfImages(numberOfImages)
			if provider.GeminiImageNumberOfImages == nil || *provider.GeminiImageNumberOfImages != numberOfImages {
				provider.GeminiImageNumberOfImages = &numberOfImages
				changed = true
			}

			outputMIMEType := "image/jpeg"
			if provider.GeminiImageOutputMIMEType != nil {
				outputMIMEType = strings.TrimSpace(*provider.GeminiImageOutputMIMEType)
			}
			outputMIMEType = normalizeGeminiImageOutputMIMEType(outputMIMEType)
			if provider.GeminiImageOutputMIMEType == nil || *provider.GeminiImageOutputMIMEType != outputMIMEType {
				provider.GeminiImageOutputMIMEType = &outputMIMEType
				changed = true
			}
		}
	}
	return changed
}

func normalizeGeminiThinkingLevel(model, level string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	level = strings.ToLower(strings.TrimSpace(level))

	// Gemini 3 Flash supports: minimal/low/medium/high.
	if strings.Contains(model, "gemini-3") && strings.Contains(model, "flash") {
		switch level {
		case "minimal", "low", "medium", "high":
			return level
		default:
			return "low"
		}
	}

	// Default: low/high.
	if level == "high" {
		return "high"
	}
	return "low"
}

func normalizeAnthropicPromptCacheTTL(ttl string) string {
	switch strings.ToLower(strings.TrimSpace(ttl)) {
	case "1h":
		return "1h"
	case "5m", "":
		return "5m"
	default:
		return "5m"
	}
}

func geminiThinkingBudgetRange(model string) (minBudget, maxBudget int) {
	model = strings.ToLower(strings.TrimSpace(model))

	// Defaults based on https://ai.google.dev/gemini-api/docs/thinking
	switch {
	case strings.Contains(model, "flash-lite"):
		return 512, 24576
	case strings.Contains(model, "flash"):
		return 0, 24576
	case strings.Contains(model, "pro"):
		return 128, 32768
	case strings.Contains(model, "robotics-er"):
		return 0, 24576
	default:
		return 128, 32768
	}
}

func normalizeGeminiImageAspectRatio(aspectRatio string) string {
	aspectRatio = strings.TrimSpace(aspectRatio)
	if aspectRatio == "" {
		return "1:1"
	}
	switch aspectRatio {
	case "1:1", "3:4", "4:3", "9:16", "16:9":
		return aspectRatio
	default:
		return "1:1"
	}
}

func normalizeGeminiImageSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	switch strings.ToUpper(size) {
	case "1K":
		return "1K"
	case "2K":
		return "2K"
	default:
		return ""
	}
}

func clampGeminiImageNumberOfImages(numberOfImages int) int {
	if numberOfImages < 1 {
		return 1
	}
	if numberOfImages > 8 {
		return 8
	}
	return numberOfImages
}

func normalizeGeminiImageOutputMIMEType(outputMIMEType string) string {
	outputMIMEType = strings.ToLower(strings.TrimSpace(outputMIMEType))
	if outputMIMEType == "" {
		return "image/jpeg"
	}
	switch outputMIMEType {
	case "image/jpeg", "image/png":
		return outputMIMEType
	default:
		return "image/jpeg"
	}
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
		logging.Errorf("config save failed: path=%s err=%v", m.configPath, err)
		return err
	}
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		logging.Errorf("config save failed: path=%s err=%v", m.configPath, err)
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
	m.applyConfigDefaults()
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

	m.applyConfigDefaults()
	return m.saveUnsafe()
}

func normalizeReplyWaitWindowMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch ReplyWaitWindowMode(mode) {
	case ReplyWaitWindowModeFixed:
		return string(ReplyWaitWindowModeFixed)
	case ReplyWaitWindowModeSliding:
		return string(ReplyWaitWindowModeSliding)
	default:
		return string(ReplyWaitWindowModeSliding)
	}
}

func normalizeReplyWaitWindowSeconds(seconds int) int {
	if seconds < 0 {
		return 0
	}
	if seconds > MaxReplyWaitWindowSeconds {
		return MaxReplyWaitWindowSeconds
	}
	if seconds == 0 {
		return 0
	}
	return seconds
}

func normalizeTimeZone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultTimeZone
	}
	if _, err := time.LoadLocation(value); err != nil {
		return DefaultTimeZone
	}
	return value
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

// GetImageProvider 获取生图供应商配置（优先返回第一个 gemini_image 类型）
func (m *Manager) GetImageProvider() *Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id := strings.TrimSpace(m.config.ImageProviderID)
	if id != "" {
		for i := range m.config.Providers {
			if m.config.Providers[i].ID == id && isImageGenProviderType(m.config.Providers[i].Type) {
				provider := m.config.Providers[i]
				return &provider
			}
		}
	}
	for i := range m.config.Providers {
		if m.config.Providers[i].Type == ProviderTypeGeminiImage {
			provider := m.config.Providers[i]
			return &provider
		}
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
	if errSave := m.saveUnsafe(); errSave != nil {
		return errSave
	}
	logging.Infof("provider added: id=%s name=%s type=%s", provider.ID, provider.Name, provider.Type)
	return nil
}

// UpdateProvider 更新供应商配置
func (m *Manager) UpdateProvider(provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.config.Providers {
		if m.config.Providers[i].ID == provider.ID {
			if m.config.ActiveProviderID == provider.ID && !isChatProviderType(provider.Type) && isChatProviderType(m.config.Providers[i].Type) {
				return ErrProviderNotChatCapable
			}
			m.config.Providers[i] = provider
			if m.config.ImageProviderID == provider.ID && !isImageGenProviderType(provider.Type) {
				m.config.ImageProviderID = ""
			}
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			logging.Infof("provider updated: id=%s name=%s", provider.ID, provider.Name)
			return nil
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
				nextID := ""
				for _, p := range m.config.Providers {
					if isChatProviderType(p.Type) {
						nextID = p.ID
						break
					}
				}
				if nextID == "" {
					nextID = m.config.Providers[0].ID
				}
				m.config.ActiveProviderID = nextID
			}
			if m.config.ImageProviderID == id {
				m.config.ImageProviderID = ""
			}
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			logging.Infof("provider deleted: id=%s", id)
			return nil
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
			if !isChatProviderType(p.Type) {
				return ErrProviderNotChatCapable
			}
			m.config.ActiveProviderID = id
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			logging.Infof("active provider changed: id=%s", id)
			return nil
		}
	}
	return ErrProviderNotFound
}

// SetImageProvider 设置生图供应商（清空表示自动选择第一个 gemini_image）
func (m *Manager) SetImageProvider(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		m.config.ImageProviderID = ""
		return m.saveUnsafe()
	}

	for _, p := range m.config.Providers {
		if p.ID == id {
			if !isImageGenProviderType(p.Type) {
				return ErrProviderNotImageGenCapable
			}
			m.config.ImageProviderID = id
			if errSave := m.saveUnsafe(); errSave != nil {
				return errSave
			}
			logging.Infof("image provider changed: id=%s", id)
			return nil
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

func (m *Manager) GetWeatherDefaultCity() *WeatherCity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneWeatherCity(m.config.WeatherDefaultCity)
}

// GetActiveProviderID 获取激活的供应商ID
func (m *Manager) GetActiveProviderID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.ActiveProviderID
}

// GetClawBotConfig 获取微信 ClawBot 配置
func (m *Manager) GetClawBotConfig() ClawBotConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.ClawBot
}

// UpdateClawBotConfig 更新微信 ClawBot 配置
func (m *Manager) UpdateClawBotConfig(cfg ClawBotConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ClawBot = cfg
	if m.applyConfigDefaults() {
		return m.saveUnsafe()
	}
	return m.saveUnsafe()
}
