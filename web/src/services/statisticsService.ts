import type { ApiResponse } from '../types/chat'
import type { StatisticsQuery, StatisticsSummary } from '../types/statistics'
import { translate } from '../i18n'
import { apiFetchJson } from './api'

export const statisticsService = {
    async getStatistics(query: StatisticsQuery = {}): Promise<StatisticsSummary> {
        const params = new URLSearchParams()
        for (const key of ['from', 'to', 'provider_id', 'model'] as const) {
            const value = query[key]
            if (value !== undefined && value !== '') params.set(key, value)
        }
        const suffix = params.size ? `?${params.toString()}` : ''
        const response = await apiFetchJson<ApiResponse<StatisticsSummary>>(`/management/statistics${suffix}`)
        if (!response.success || !response.data) {
            throw new Error(response.error || translate('common.loadFailed'))
        }
        return response.data
    },
}
