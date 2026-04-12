package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	thinkBlockRegexp = regexp.MustCompile(`(?is)<think[^>]*>[\s\S]*?<\/think\s*>`)
	thinkCloseRegexp = regexp.MustCompile(`(?is)<\/think\s*>`)
)

func normalizeAssistantContent(content string) string {
	if content == "" {
		return ""
	}
	withoutBlocks := thinkBlockRegexp.ReplaceAllString(content, "")
	lower := strings.ToLower(withoutBlocks)
	openIndex := strings.Index(lower, "<think")
	if openIndex != -1 {
		return withoutBlocks[:openIndex]
	}
	return thinkCloseRegexp.ReplaceAllString(withoutBlocks, "")
}

func configuredAssistantMessageSplitToken(cm *config.Manager) string {
	if cm == nil {
		return config.DefaultAssistantMessageSplitToken
	}
	return cm.Get().AssistantMessageSplitToken
}

func splitAssistantMessageContent(content string, splitToken string) []string {
	normalized := normalizeAssistantContent(content)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil
	}

	token := strings.TrimSpace(splitToken)
	if token == "" || !strings.Contains(normalized, token) {
		return []string{normalized}
	}

	parts := strings.Split(normalized, token)
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments = append(segments, part)
	}
	if len(segments) == 0 {
		return nil
	}
	return segments
}

func (h *Handler) maybeGenerateTTSAudio(ctx context.Context, assistantContent string) []string {
	cfg := h.configManager.Get()
	if !cfg.TTSEnabled || cfg.TTSProvider == nil {
		return nil
	}

	provider := cfg.TTSProvider
	if provider.Type != config.TTSProviderTypeMinimax {
		return nil
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return nil
	}
	if strings.TrimSpace(provider.Model) == "" {
		return nil
	}
	if strings.TrimSpace(provider.VoiceSetting.VoiceID) == "" {
		return nil
	}
	if h.ttsAudioDir == "" {
		return nil
	}

	segments := splitAssistantMessageContent(assistantContent, configuredAssistantMessageSplitToken(h.configManager))
	if len(segments) == 0 {
		return nil
	}

	ttsClient := client.NewMinimaxTTSClient(provider.BaseURL, provider.APIKey)

	paths := make([]string, len(segments))
	for i, segment := range segments {
		audioBytes, errTTS := ttsClient.TextToMP3(ctx, provider.Model, segment, provider.VoiceSetting.VoiceID, provider.VoiceSetting.Speed, provider.LanguageBoost)
		if errTTS != nil {
			logging.Warnf("tts generation failed: err=%v segment=%s", errTTS, logging.Truncate(segment, 80))
			continue
		}

		filename := generateID() + ".mp3"
		filePath := filepath.Join(h.ttsAudioDir, filename)
		if errWrite := os.WriteFile(filePath, audioBytes, 0644); errWrite != nil {
			logging.Warnf("tts audio save failed: err=%v", errWrite)
			continue
		}

		paths[i] = path.Join(ttsAudioDirName, filename)
	}

	allEmpty := true
	for _, p := range paths {
		if p != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return nil
	}
	return paths
}
