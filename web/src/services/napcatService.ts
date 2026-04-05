import type { ApiResponse } from '../types/chat'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export interface NapCatSettings {
    enabled: boolean
    access_token?: string
    has_access_token: boolean
    prompt_id?: string
    prompt_name?: string
    allow_private: boolean
    source_filter_mode: 'all' | 'allowlist' | string
    allowed_private_user_ids?: string[]
    status: 'disabled' | 'missing_token' | 'connected' | 'disconnected' | 'error' | string
    self_id?: string
    nickname?: string
    last_error?: string
    last_error_at?: string
}

export const napCatService = {
    async getSettings(): Promise<NapCatSettings> {
        const data = await apiFetchJson<ApiResponse<NapCatSettings>>('/api/settings/napcat')
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getNapCatSettingsFailed'))
        }
        return data.data
    },

    async updateSettings(update: {
        enabled: boolean
        access_token?: string
        clear_access_token?: boolean
        prompt_id?: string
        allow_private: boolean
        source_filter_mode: string
        allowed_private_user_ids: string[]
    }): Promise<NapCatSettings> {
        const data = await apiFetchJson<ApiResponse<NapCatSettings>>('/api/settings/napcat', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(update),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.saveNapCatSettingsFailed'))
        }
        return data.data
    },
}
