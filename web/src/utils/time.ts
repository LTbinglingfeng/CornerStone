export function formatTime(dateStr: string, locale = 'zh-CN'): string {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))

    if (diffDays <= 0) {
        return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    }
    if (diffDays === 1) return '昨天'
    if (diffDays < 7) {
        const weekdays = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']
        return weekdays[date.getDay()]
    }
    return date.toLocaleDateString(locale, { month: 'numeric', day: 'numeric' })
}

export function formatRelativeTime(dateStr: string): string {
    const date = new Date(dateStr)
    if (Number.isNaN(date.getTime())) return ''

    const now = Date.now()
    const diffMs = now - date.getTime()
    if (diffMs < 0) return formatTime(dateStr)

    const diffSeconds = Math.floor(diffMs / 1000)
    if (diffSeconds < 30) return '刚刚'
    if (diffSeconds < 60) return `${diffSeconds}秒前`

    const diffMinutes = Math.floor(diffSeconds / 60)
    if (diffMinutes < 60) return `${diffMinutes}分钟前`

    const diffHours = Math.floor(diffMinutes / 60)
    if (diffHours < 24) return `${diffHours}小时前`

    const diffDays = Math.floor(diffHours / 24)
    if (diffDays === 1) return '昨天'
    if (diffDays < 7) return `${diffDays}天前`

    return formatTime(dateStr)
}
