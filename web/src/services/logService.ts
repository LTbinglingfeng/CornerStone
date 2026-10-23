import type { ApiResponse } from '../types/chat'
import type { LogPage, LogQuery } from '../types/logs'
import { translate } from '../i18n'
import { apiFetch, apiFetchJson } from './api'

const LOGS_PATH = '/api/logs'

const buildQuery = (query: LogQuery) => {
    const params = new URLSearchParams()
    if (query.levels && query.levels.length > 0) params.set('level', query.levels.join(','))
    const search = query.search?.trim()
    if (search) params.set('q', search)
    for (const prefix of query.excludeEventPrefixes || []) params.append('exclude_event', prefix)
    if (query.limit) params.set('limit', String(query.limit))
    if (typeof query.before === 'number') params.set('before', String(query.before))
    if (typeof query.after === 'number') params.set('after', String(query.after))
    const text = params.toString()
    return text ? `?${text}` : ''
}

export const logService = {
    async getLogs(query: LogQuery = {}): Promise<LogPage> {
        const data = await apiFetchJson<ApiResponse<LogPage>>(`${LOGS_PATH}${buildQuery(query)}`)
        if (!data.success || !data.data) {
            throw new Error(data.error || translate('logs.loadFailed'))
        }
        return { ...data.data, entries: data.data.entries || [] }
    },

    async downloadLogFile(): Promise<void> {
        const res = await apiFetch(`${LOGS_PATH}/download`)
        const disposition = res.headers.get('Content-Disposition') || ''
        const name = /filename="?([^";]+)"?/.exec(disposition)?.[1] || 'cornerstone.log'
        const url = URL.createObjectURL(await res.blob())
        const link = document.createElement('a')
        link.href = url
        link.download = name
        document.body.appendChild(link)
        link.click()
        link.remove()
        window.setTimeout(() => URL.revokeObjectURL(url), 1000)
    },
}
