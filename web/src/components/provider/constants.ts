import type { ProviderType } from '../../types/chat'
import { translate } from '../../i18n'

export type SelectOption = { value: string; label: string }

export const getProviderTypesAll = (): { value: ProviderType; label: string }[] => [
    { value: 'openai', label: translate('provider.openaiCompatible') },
    { value: 'openai_response', label: translate('provider.openaiResponses') },
    { value: 'gemini', label: translate('provider.googleGemini') },
    { value: 'gemini_image', label: translate('provider.geminiImage') },
    { value: 'anthropic', label: translate('provider.anthropicClaude') },
]

export const getProviderTypesChat = (): { value: ProviderType; label: string }[] => [
    { value: 'openai', label: translate('provider.openaiCompatible') },
    { value: 'openai_response', label: translate('provider.openaiResponses') },
    { value: 'gemini', label: translate('provider.googleGemini') },
    { value: 'anthropic', label: translate('provider.anthropicClaude') },
]

export const getOpenAIReasoningEffortOptions = (): SelectOption[] => [
    { value: '', label: translate('provider.defaultEffort') },
    { value: 'low', label: translate('provider.lowEffort') },
    { value: 'medium', label: translate('provider.mediumEffort') },
    { value: 'high', label: translate('provider.highEffort') },
]

export const getAnthropicPromptCacheTTLOptions = (): SelectOption[] => [
    { value: '5m', label: translate('provider.promptCacheTTL5m') },
    { value: '1h', label: translate('provider.promptCacheTTL1h') },
]

export const getGeminiThinkingModes = (): SelectOption[] => [
    { value: 'none', label: translate('provider.noThinking') },
    { value: 'thinking_level', label: translate('provider.thinkingLevelGemini3') },
    { value: 'thinking_budget', label: translate('provider.thinkingBudgetGemini25') },
]

export const getGeminiThinkingLevels = (): SelectOption[] => [
    { value: 'low', label: translate('provider.lowEffort') },
    { value: 'high', label: translate('provider.highEffort') },
]

export const GEMINI_IMAGE_ASPECT_RATIOS: SelectOption[] = [
    { value: '1:1', label: '1:1' },
    { value: '3:4', label: '3:4' },
    { value: '4:3', label: '4:3' },
    { value: '9:16', label: '9:16' },
    { value: '16:9', label: '16:9' },
]

export const getGeminiImageSizes = (): SelectOption[] => [
    { value: '', label: translate('provider.defaultEffort') },
    { value: '1K', label: '1K' },
    { value: '2K', label: '2K' },
]

export const GEMINI_IMAGE_OUTPUT_MIME_TYPES: SelectOption[] = [
    { value: 'image/jpeg', label: 'image/jpeg' },
    { value: 'image/png', label: 'image/png' },
]
