import type { Locale } from '../../i18n'

export type UsageRange = 'today' | 'week' | 'month' | 'all'

export const USAGE_RANGES: UsageRange[] = ['today', 'week', 'month', 'all']

const RANGE_DAYS: Record<Exclude<UsageRange, 'all'>, number> = { today: 1, week: 7, month: 30 }

/** Ranges start at local midnight so "Today" matches the user's day, not UTC. */
export function rangeStart(range: UsageRange, now = new Date()): Date | null {
    if (range === 'all') return null
    return new Date(now.getFullYear(), now.getMonth(), now.getDate() - (RANGE_DAYS[range] - 1))
}

const intlLocale = (locale: Locale) => (locale === 'zh' ? 'zh-CN' : 'en-US')
const formatters = new Map<string, Intl.NumberFormat>()
const numberFormat = (locale: Locale, options: Intl.NumberFormatOptions) => {
    const key = `${locale}:${JSON.stringify(options)}`
    let formatter = formatters.get(key)
    if (!formatter) {
        formatter = new Intl.NumberFormat(intlLocale(locale), options)
        formatters.set(key, formatter)
    }
    return formatter
}

export const formatNumber = (value: number, locale: Locale) => numberFormat(locale, {}).format(value)

/** Exact below 10,000, where every digit is still readable; compact (1.2万 / 12.3K) above. */
export const formatCompact = (value: number, locale: Locale) =>
    value < 10_000
        ? formatNumber(value, locale)
        : numberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(value)

export const formatPercent = (ratio: number | null, locale: Locale) =>
    ratio === null ? null : numberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(ratio)

export function formatDuration(ms: number, locale: Locale) {
    const unit = (value: number, name: string, digits: number) =>
        numberFormat(locale, {
            style: 'unit',
            unit: name,
            unitDisplay: 'narrow',
            maximumFractionDigits: digits,
        }).format(value)
    if (ms < 1000) return unit(Math.round(ms), 'millisecond', 0)
    if (ms < 60_000) return unit(ms / 1000, 'second', 1)
    if (ms < 3_600_000) return unit(ms / 60_000, 'minute', 1)
    return unit(ms / 3_600_000, 'hour', 1)
}

export const formatClock = (iso: string, locale: Locale) => {
    const date = new Date(iso)
    return Number.isNaN(date.getTime())
        ? ''
        : date.toLocaleTimeString(intlLocale(locale), { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export const ratio = (part: number, whole: number) => (whole > 0 ? Math.min(1, part / whole) : null)
