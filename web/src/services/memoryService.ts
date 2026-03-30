import type { ApiResponse } from '../types/chat'
import { translate } from '../i18n'
import type { Memory } from '../types/memory'
import { apiFetchJson } from './api'

export interface MemoryExtractionSettings {
    rounds: number
    max_rounds: number
    refresh_interval: number
    max_refresh_interval: number
    default_refresh_interval: number
    provider_id?: string
    provider_name?: string
    provider_context_messages?: number
}

export interface MemoryExtractionPromptTemplate {
    template: string
    default_template: string
}

export interface MemoryExportItem {
    subject: 'user' | 'self'
    category: string
    content: string
    strength: number
    seen_count: number
    pinned: boolean
}

export interface MemoryStats {
    total: number
    active: number
    weak: number
    archived: number
    pinned: number
    by_subject: Record<string, number>
    by_category: Record<string, number>
    avg_strength: number
    total_seen_count: number
}

export interface MemoryImportResult {
    added: number
    invalid: number
    mode: string
}

export const memoryService = {
    async getMemories(promptId: string): Promise<Memory[]> {
        const data = await apiFetchJson<ApiResponse<Memory[]>>(`/api/memory/${encodeURIComponent(promptId)}`)
        if (!data.success) {
            throw new Error(data.error || translate('service.getMemoryFailed'))
        }
        return data.data || []
    },

    async addMemory(promptId: string, memory: Partial<Memory>): Promise<Memory[]> {
        const data = await apiFetchJson<ApiResponse<Memory[]>>(`/api/memory/${encodeURIComponent(promptId)}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(memory),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.addMemoryFailed'))
        }
        return data.data || []
    },

    async updateMemory(promptId: string, memoryId: string, memory: Partial<Memory>): Promise<Memory[]> {
        const data = await apiFetchJson<ApiResponse<Memory[]>>(
            `/api/memory/${encodeURIComponent(promptId)}/${encodeURIComponent(memoryId)}`,
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(memory),
            }
        )
        if (!data.success) {
            throw new Error(data.error || translate('service.updateMemoryFailed'))
        }
        return data.data || []
    },

    async deleteMemory(promptId: string, memoryId: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>(
            `/api/memory/${encodeURIComponent(promptId)}/${encodeURIComponent(memoryId)}`,
            {
                method: 'DELETE',
            }
        )
        if (!data.success) {
            throw new Error(data.error || translate('service.deleteMemoryFailed'))
        }
    },

    async setMemoryProvider(providerId: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>('/api/settings/memory-provider', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider_id: providerId }),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.setMemoryModelFailed'))
        }
    },

    async setMemoryEnabled(enabled: boolean): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>('/api/settings/memory-enabled', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled }),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.setMemorySwitchFailed'))
        }
    },

    async getMemoryExtractionSettings(): Promise<MemoryExtractionSettings> {
        const data = await apiFetchJson<ApiResponse<MemoryExtractionSettings>>('/api/settings/memory-extraction')
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getMemoryExtractionSettingsFailed'))
        }
        return data.data
    },

    async setMemoryExtractionRounds(rounds: number): Promise<MemoryExtractionSettings> {
        const data = await apiFetchJson<ApiResponse<MemoryExtractionSettings>>('/api/settings/memory-extraction', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ rounds }),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.setMemoryExtractionRoundsFailed'))
        }
        const next = await memoryService.getMemoryExtractionSettings()
        return next
    },

    async setMemoryRefreshInterval(refresh_interval: number): Promise<MemoryExtractionSettings> {
        const data = await apiFetchJson<ApiResponse<MemoryExtractionSettings>>('/api/settings/memory-extraction', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ refresh_interval }),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.setMemoryRefreshIntervalFailed'))
        }
        const next = await memoryService.getMemoryExtractionSettings()
        return next
    },

    async getMemoryExtractionPromptTemplate(): Promise<MemoryExtractionPromptTemplate> {
        const data = await apiFetchJson<ApiResponse<MemoryExtractionPromptTemplate>>(
            '/api/settings/memory-extraction-prompt'
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getMemoryExtractionPromptFailed'))
        }
        return data.data
    },

    async updateMemoryExtractionPromptTemplate(template: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>('/api/settings/memory-extraction-prompt', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ template }),
        })
        if (!data.success) {
            throw new Error(data.error || translate('service.saveMemoryExtractionPromptFailed'))
        }
    },

    async getMemoryStats(promptId: string): Promise<MemoryStats> {
        const data = await apiFetchJson<ApiResponse<MemoryStats>>(`/api/memory/${encodeURIComponent(promptId)}/stats`)
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.getMemoryStatsFailed'))
        }
        return data.data
    },

    async exportMemories(promptId: string): Promise<MemoryExportItem[]> {
        const data = await apiFetchJson<ApiResponse<MemoryExportItem[]>>(
            `/api/memory/${encodeURIComponent(promptId)}/export`
        )
        if (!data.success) {
            throw new Error(data.error || translate('service.exportMemoryFailed'))
        }
        return data.data || []
    },

    async batchDeleteMemories(promptId: string, ids: string[]): Promise<{ deleted: number }> {
        const data = await apiFetchJson<ApiResponse<{ deleted: number }>>(
            `/api/memory/${encodeURIComponent(promptId)}/batch`,
            {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ids }),
            }
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.batchDeleteFailed'))
        }
        return data.data
    },

    async deleteArchivedMemories(promptId: string): Promise<{ deleted: number }> {
        const data = await apiFetchJson<ApiResponse<{ deleted: number }>>(
            `/api/memory/${encodeURIComponent(promptId)}/archived`,
            {
                method: 'DELETE',
            }
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.clearArchivedFailed'))
        }
        return data.data
    },

    async importMemories(
        promptId: string,
        memories: MemoryExportItem[],
        mode: 'merge' | 'replace' = 'merge'
    ): Promise<MemoryImportResult> {
        const data = await apiFetchJson<ApiResponse<MemoryImportResult>>(
            `/api/memory/${encodeURIComponent(promptId)}/import`,
            {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ memories, mode }),
            }
        )
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('service.importMemoryFailed'))
        }
        return data.data
    },
}
