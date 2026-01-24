import type { ApiResponse } from '../types/chat'
import { apiFetchJson } from './api'

export type TTSProviderType = 'minimax'

export interface TTSVoiceSetting {
    voice_id: string
    speed: number
}

export interface TTSProviderConfig {
    type: TTSProviderType
    base_url: string
    api_key: string
    model: string
    voice_setting: TTSVoiceSetting
    language_boost?: string
}

export interface TTSSettings {
    enabled: boolean
    provider: TTSProviderConfig | null
}

export const ttsService = {
    async getTTSSettings(): Promise<TTSSettings> {
        const data = await apiFetchJson<ApiResponse<TTSSettings>>('/api/settings/tts')
        if (!data.success || !data.data) {
            throw new Error(data.error || '获取TTS设置失败')
        }
        return {
            enabled: !!data.data.enabled,
            provider: data.data.provider || null,
        }
    },

    async updateTTSSettings(update: { enabled?: boolean; provider?: TTSProviderConfig | null }): Promise<TTSSettings> {
        const payload: Record<string, unknown> = {}
        if (update.enabled !== undefined) {
            payload.enabled = update.enabled
        }
        if (update.provider !== undefined) {
            payload.provider = update.provider
        }

        const data = await apiFetchJson<ApiResponse<TTSSettings>>('/api/settings/tts', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || '保存TTS设置失败')
        }
        return {
            enabled: !!data.data.enabled,
            provider: data.data.provider || null,
        }
    },
}

