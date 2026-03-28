import type { ApiResponse } from '../types/chat'
import { apiFetchJson } from './api'

export interface ClawBotSettings {
    enabled: boolean
    base_url: string
    bot_token?: string
    has_bot_token: boolean
    ilink_user_id?: string
    prompt_id?: string
    prompt_name?: string
    status: 'disabled' | 'missing_token' | 'running' | 'error' | 'stopped' | string
    polling: boolean
    last_error?: string
    last_error_at?: string
}

export interface ClawBotQRCodeStartResponse {
    session_id: string
    qrcode: string
    qrcode_img_content?: string
}

export interface ClawBotQRCodePollResponse {
    status: 'wait' | 'scaned' | 'confirmed' | 'expired' | string
    settings?: ClawBotSettings
}

export const clawBotService = {
    async getSettings(): Promise<ClawBotSettings> {
        const data = await apiFetchJson<ApiResponse<ClawBotSettings>>('/api/settings/clawbot')
        if (!data.success || !data.data) {
            throw new Error(data.error || '获取 ClawBot 设置失败')
        }
        return data.data
    },

    async updateSettings(update: {
        enabled: boolean
        base_url: string
        bot_token?: string
        prompt_id?: string
        clear_bot_token?: boolean
    }): Promise<ClawBotSettings> {
        const data = await apiFetchJson<ApiResponse<ClawBotSettings>>('/api/settings/clawbot', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(update),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || '保存 ClawBot 设置失败')
        }
        return data.data
    },

    async startQRCode(baseURL: string): Promise<ClawBotQRCodeStartResponse> {
        const data = await apiFetchJson<ApiResponse<ClawBotQRCodeStartResponse>>('/api/settings/clawbot/qr-start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ base_url: baseURL }),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || '获取二维码失败')
        }
        return data.data
    },

    async pollQRCode(sessionID: string): Promise<ClawBotQRCodePollResponse> {
        const data = await apiFetchJson<ApiResponse<ClawBotQRCodePollResponse>>('/api/settings/clawbot/qr-poll', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: sessionID }),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || '轮询二维码状态失败')
        }
        return data.data
    },
}
