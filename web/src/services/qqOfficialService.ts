import type { ApiResponse } from '../types/chat'
import type { QQOfficialSettings, QQOfficialUpdate, QQOfficialLogin, QQOfficialLoginStatus } from '../types/qqOfficial'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const result = await apiFetchJson<ApiResponse<T>>(path, init)
    if (!result.success || result.data == null) throw new Error(result.error || translate('qqOfficial.requestFailed'))
    return result.data
}
const json = (method: string, body: unknown, signal?: AbortSignal): RequestInit => ({
    method,
    signal,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
})
export const qqOfficialService = {
    getSettings: (signal?: AbortSignal) => request<QQOfficialSettings>('/api/settings/qq-official', { signal }),
    updateSettings: (update: QQOfficialUpdate, signal?: AbortSignal) =>
        request<QQOfficialSettings>('/api/settings/qq-official', json('PUT', update, signal)),
    start: (signal?: AbortSignal) =>
        request<QQOfficialLogin>('/api/channel/qq-official/login/start', json('POST', {}, signal)),
    poll: (id: string, signal?: AbortSignal) =>
        request<QQOfficialLoginStatus>(`/api/channel/qq-official/login/status?id=${encodeURIComponent(id)}`, {
            signal,
        }),
    async cancel(id: string) {
        const result = await apiFetchJson<ApiResponse<QQOfficialLoginStatus>>('/api/channel/qq-official/login/cancel', {
            ...json('POST', { id }),
            keepalive: true,
        })
        if (!result.success || !result.data) throw new Error(result.error || translate('qqOfficial.requestFailed'))
        return result.data
    },
}
