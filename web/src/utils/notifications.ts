const STORAGE_KEY = 'cornerstone.notifications.enabled'

export function isNotificationSupported(): boolean {
    return typeof window !== 'undefined' && 'Notification' in window
}

export function getNotificationsEnabled(): boolean {
    try {
        const raw = localStorage.getItem(STORAGE_KEY)
        if (!raw) return false
        if (raw === 'true' || raw === '1') return true
        if (raw === 'false' || raw === '0') return false
        return JSON.parse(raw) === true
    } catch {
        return false
    }
}

export function setNotificationsEnabled(enabled: boolean): void {
    localStorage.setItem(STORAGE_KEY, enabled ? 'true' : 'false')
}

export async function requestNotificationPermission(): Promise<NotificationPermission | 'unsupported'> {
    if (!isNotificationSupported()) return 'unsupported'
    if (Notification.permission !== 'default') return Notification.permission
    try {
        return await Notification.requestPermission()
    } catch {
        return Notification.permission
    }
}

export function formatNotificationBody(content: string, maxLength = 200): string {
    const normalized = content.replace(/\s+/g, ' ').trim()
    if (normalized.length <= maxLength) return normalized
    return normalized.slice(0, maxLength) + '...'
}
