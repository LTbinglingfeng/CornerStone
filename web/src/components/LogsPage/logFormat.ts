import type { LogEntry, LogField, LogLevel } from '../../types/logs'
import type { Tone } from '../ui'

export type LevelKey = LogLevel | 'OTHER'

export const levelKey = (entry: LogEntry): LevelKey => {
    switch (entry.level) {
        case 'INFO':
        case 'WARN':
        case 'ERROR':
        case 'FATAL':
            return entry.level
        default:
            return 'OTHER'
    }
}

const pad = (value: number, size = 2) => String(value).padStart(size, '0')

export const parseEntryTime = (entry: LogEntry): Date | null => {
    if (!entry.time) return null
    const date = new Date(entry.time)
    return Number.isNaN(date.getTime()) ? null : date
}

/** Microseconds survive in the RFC 3339 string even though Date keeps only milliseconds. */
export const formatClock = (entry: LogEntry, date: Date | null) => {
    if (!date) return ''
    const fraction = /\.(\d{1,3})/.exec(entry.time || '')?.[1]?.padEnd(3, '0') ?? pad(date.getMilliseconds(), 3)
    return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${fraction}`
}

export const dayKey = (date: Date) => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`

export const formatDate = (date: Date, locale: string) =>
    date.toLocaleDateString(locale, { year: 'numeric', month: '2-digit', day: '2-digit' })

export const formatBytes = (bytes: number) => {
    if (!Number.isFinite(bytes) || bytes < 0) return '—'
    if (bytes < 1024) return `${bytes} B`
    const units = ['KB', 'MB', 'GB']
    let value = bytes / 1024
    let unit = 0
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024
        unit += 1
    }
    return `${value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`
}

const FAILURE = new Set(['error', 'failed', 'partial_failed', 'write_failed', 'aborted', 'timeout'])
const CAUTION = new Set(['uncertain', 'unconfirmed', 'partial', 'rejected', 'canceled'])

/** Colour only signals problems; success is the normal case and stays neutral. */
export const fieldTone = (field: LogField): Tone => {
    const value = field.value.toLowerCase()
    if (field.key === 'http_status') {
        const status = Number(field.value)
        if (status >= 500) return 'danger'
        if (status >= 400) return 'warning'
        return 'neutral'
    }
    if (field.key === 'status' || field.key.endsWith('_status')) {
        if (FAILURE.has(value)) return 'danger'
        if (CAUTION.has(value)) return 'warning'
    }
    if ((field.key === 'error_kind' || field.key === 'write_error') && value !== 'none' && value !== '') {
        return value === 'canceled' ? 'warning' : 'danger'
    }
    if (field.key === 'err' || field.key === 'error') return 'danger'
    return 'neutral'
}

/** `compact` is for the one-line summary; details always show the exact value. */
export const formatFieldValue = (field: LogField, compact = false) => {
    if (field.value === '') return '""'
    if (compact && field.key === 'request' && /^[0-9a-f-]{36}$/i.test(field.value)) return field.value.slice(0, 8)
    if (field.key.endsWith('_ms') && /^\d+$/.test(field.value)) {
        const ms = Number(field.value)
        return ms >= 1000 ? `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 1)} s` : `${ms} ms`
    }
    if (field.key.endsWith('_bytes') && /^\d+$/.test(field.value)) return formatBytes(Number(field.value))
    return field.value
}

export const findField = (entry: LogEntry, key: string) => entry.fields?.find((field) => field.key === key)?.value
