import type { ApiResponse } from '../types/chat'
import type { Memory } from '../types/memory'
import { apiFetchJson } from './api'

export const memoryService = {
    async getMemories(promptId: string): Promise<Memory[]> {
        const data = await apiFetchJson<ApiResponse<Memory[]>>(`/api/memory/${encodeURIComponent(promptId)}`)
        if (!data.success) {
            throw new Error(data.error || '获取记忆失败')
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
            throw new Error(data.error || '添加记忆失败')
        }
        return data.data || []
    },

    async updateMemory(promptId: string, memoryId: string, memory: Partial<Memory>): Promise<Memory[]> {
        const data = await apiFetchJson<ApiResponse<Memory[]>>(`/api/memory/${encodeURIComponent(promptId)}/${encodeURIComponent(memoryId)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(memory),
        })
        if (!data.success) {
            throw new Error(data.error || '更新记忆失败')
        }
        return data.data || []
    },

    async deleteMemory(promptId: string, memoryId: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>(`/api/memory/${encodeURIComponent(promptId)}/${encodeURIComponent(memoryId)}`, {
            method: 'DELETE',
        })
        if (!data.success) {
            throw new Error(data.error || '删除记忆失败')
        }
    },

    async setMemoryProvider(providerId: string): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>('/api/settings/memory-provider', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ provider_id: providerId }),
        })
        if (!data.success) {
            throw new Error(data.error || '设置记忆模型失败')
        }
    },

    async setMemoryEnabled(enabled: boolean): Promise<void> {
        const data = await apiFetchJson<ApiResponse<unknown>>('/api/settings/memory-enabled', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled }),
        })
        if (!data.success) {
            throw new Error(data.error || '设置记忆开关失败')
        }
    },
}

