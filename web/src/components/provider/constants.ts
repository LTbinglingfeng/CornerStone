import type { ProviderType } from '../../types/chat'

export type SelectOption = { value: string; label: string }

export const PROVIDER_TYPES_ALL: { value: ProviderType; label: string }[] = [
    { value: 'openai', label: 'OpenAI 兼容' },
    { value: 'openai_response', label: 'OpenAI Responses' },
    { value: 'gemini', label: 'Google Gemini' },
    { value: 'gemini_image', label: 'Gemini 生图（备用）' },
    { value: 'anthropic', label: 'Anthropic Claude' },
]

export const PROVIDER_TYPES_CHAT: { value: ProviderType; label: string }[] = [
    { value: 'openai', label: 'OpenAI 兼容' },
    { value: 'openai_response', label: 'OpenAI Responses' },
    { value: 'gemini', label: 'Google Gemini' },
    { value: 'anthropic', label: 'Anthropic Claude' },
]

export const OPENAI_REASONING_EFFORT_OPTIONS: SelectOption[] = [
    { value: '', label: '默认' },
    { value: 'low', label: '低 (low)' },
    { value: 'medium', label: '中 (medium)' },
    { value: 'high', label: '高 (high)' },
]

export const GEMINI_THINKING_MODES: SelectOption[] = [
    { value: 'none', label: '不思考' },
    { value: 'thinking_level', label: 'thinkingLevel (Gemini 3 系列)' },
    { value: 'thinking_budget', label: 'thinkingBudget (Gemini 2.5 系列)' },
]

export const GEMINI_THINKING_LEVELS: SelectOption[] = [
    { value: 'low', label: '低 (low)' },
    { value: 'high', label: '高 (high)' },
]

export const GEMINI_IMAGE_ASPECT_RATIOS: SelectOption[] = [
    { value: '1:1', label: '1:1' },
    { value: '3:4', label: '3:4' },
    { value: '4:3', label: '4:3' },
    { value: '9:16', label: '9:16' },
    { value: '16:9', label: '16:9' },
]

export const GEMINI_IMAGE_SIZES: SelectOption[] = [
    { value: '', label: '默认' },
    { value: '1K', label: '1K' },
    { value: '2K', label: '2K' },
]

export const GEMINI_IMAGE_OUTPUT_MIME_TYPES: SelectOption[] = [
    { value: 'image/jpeg', label: 'image/jpeg' },
    { value: 'image/png', label: 'image/png' },
]
