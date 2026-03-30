import { getLocale, translations, type Locale } from '../i18n'

function getTimeStrings(locale: Locale) {
    return translations[locale].time
}

export function formatTime(dateStr: string, locale: Locale = getLocale()): string {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
    const t = getTimeStrings(locale)
    const dateLocale = locale === 'en' ? 'en-US' : 'zh-CN'

    if (diffDays <= 0) {
        return date.toLocaleTimeString(dateLocale, { hour: '2-digit', minute: '2-digit' })
    }
    if (diffDays === 1) return t.yesterday
    if (diffDays < 7) {
        return t.weekdays[date.getDay()]
    }
    return date.toLocaleDateString(dateLocale, { month: 'numeric', day: 'numeric' })
}

export function formatRelativeTime(dateStr: string, locale: Locale = getLocale()): string {
    const date = new Date(dateStr)
    if (Number.isNaN(date.getTime())) return ''

    const now = Date.now()
    const diffMs = now - date.getTime()
    if (diffMs < 0) return formatTime(dateStr, locale)
    const t = getTimeStrings(locale)

    const diffSeconds = Math.floor(diffMs / 1000)
    if (diffSeconds < 30) return t.justNow
    if (diffSeconds < 60) return t.secondsAgo.replace('{{count}}', String(diffSeconds))

    const diffMinutes = Math.floor(diffSeconds / 60)
    if (diffMinutes < 60) return t.minutesAgo.replace('{{count}}', String(diffMinutes))

    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return t.hoursAgo.replace('{{count}}', String(diffHours))

    const diffDays = Math.floor(diffHours / 24)
    if (diffDays === 1) return t.yesterday
    if (diffDays < 7) return t.daysAgo.replace('{{count}}', String(diffDays))

    return formatTime(dateStr, locale)
}
