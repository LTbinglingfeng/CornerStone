import type { ApiResponse } from '../types/chat'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export type ClawBotCommandPermissionKey = 'new' | 'ls' | 'checkout' | 'rename' | 'delete' | 'prompt' | 're'

export type ClawBotCommandPermissions = Record<ClawBotCommandPermissionKey, boolean>

export interface ClawBotSettings {
    enabled: boolean
    base_url: string
    bot_token?: string
    has_bot_token: boolean
    ilink_user_id?: string
    prompt_id?: string
    prompt_name?: string
    command_permissions: ClawBotCommandPermissions
    tool_progress: boolean
    status: 'disabled' | 'missing_token' | 'connecting' | 'token_expired' | 'running' | 'error' | 'stopped' | string
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
    status:
        | 'wait'
        | 'scaned'
        | 'confirmed'
        | 'expired'
        | 'need_verifycode'
        | 'verify_code_blocked'
        | 'scaned_but_redirect'
        | 'binded_redirect'
        | string
    settings?: ClawBotSettings
}

export const clawBotService = {
    async getSettings(signal?: AbortSignal): Promise<ClawBotSettings> {
        const data = await apiFetchJson<ApiResponse<ClawBotSettings>>('/api/settings/clawbot', { signal })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getClawBotSettingsFailed'))
        }
        return data.data
    },

    async updateSettings(
        update: {
            enabled: boolean
            base_url: string
            bot_token?: string
            prompt_id?: string
            clear_bot_token?: boolean
            command_permissions?: ClawBotCommandPermissions
            tool_progress?: boolean
        },
        signal?: AbortSignal
    ): Promise<ClawBotSettings> {
        const data = await apiFetchJson<ApiResponse<ClawBotSettings>>('/api/settings/clawbot', {
            method: 'PUT',
            signal,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(update),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.saveClawBotSettingsFailed'))
        }
        return data.data
    },

    async startQRCode(baseURL: string, signal?: AbortSignal): Promise<ClawBotQRCodeStartResponse> {
        const data = await apiFetchJson<ApiResponse<ClawBotQRCodeStartResponse>>('/api/settings/clawbot/qr-start', {
            method: 'POST',
            signal,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ base_url: baseURL }),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getQRCodeFailed'))
        }
        return data.data
    },

    async pollQRCode(sessionID: string, verifyCode?: string, signal?: AbortSignal): Promise<ClawBotQRCodePollResponse> {
        const data = await apiFetchJson<ApiResponse<ClawBotQRCodePollResponse>>('/api/settings/clawbot/qr-poll', {
            method: 'POST',
            signal,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: sessionID, verify_code: verifyCode }),
        })
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.pollQRCodeFailed'))
        }
        return data.data
    },
}
