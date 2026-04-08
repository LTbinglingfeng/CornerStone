import type { ApiResponse } from '../types/chat'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export interface CornerstoneWebSearchProviderInfo {
    id: string
    name: string
    requires_api_key: boolean
    requires_api_host: boolean
    supports_exclude_domains: boolean
    supports_time_filter: boolean
    supports_basic_auth: boolean
    supports_max_results: boolean
}

export interface CornerstoneWebSearchProviderConfig {
    api_key?: string
    api_host?: string
    search_engine?: string
    basic_auth_username?: string
    basic_auth_password?: string
}

export interface CornerstoneWebSearchSettings {
    active_provider_id: string
    providers: Record<string, CornerstoneWebSearchProviderConfig>
    max_results: number
    fetch_results: number
    exclude_domains: string[]
    search_with_time: boolean
    timeout_seconds: number
    available_providers: CornerstoneWebSearchProviderInfo[]
}

export const cornerstoneWebSearchService = {
    async getSettings(): Promise<CornerstoneWebSearchSettings> {
        const data = await apiFetchJson<ApiResponse<CornerstoneWebSearchSettings>>(
            '/api/settings/cornerstone-web-search'
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getWebSearchSettingsFailed'))
        }
        return data.data
    },

    async updateSettings(
        patch: Partial<CornerstoneWebSearchSettings> & {
            providers?: Record<string, CornerstoneWebSearchProviderConfig>
        }
    ): Promise<CornerstoneWebSearchSettings> {
        const data = await apiFetchJson<ApiResponse<{ ok: boolean }>>('/api/settings/cornerstone-web-search', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patch),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.saveWebSearchSettingsFailed'))
        }
        return cornerstoneWebSearchService.getSettings()
    },
}
