export type AppTab = 'overview' | 'channels' | 'contacts' | 'chat' | 'settings' | 'me'

export const tabOrder: AppTab[] = ['overview', 'channels', 'contacts', 'chat', 'settings', 'me']

export const tabRoutes: Record<AppTab, string> = {
    overview: '/overview',
    channels: '/channels',
    settings: '/settings',
    chat: '/chat',
    contacts: '/contacts',
    me: '/me',
}

export const normalizePathname = (pathname: string) => {
    const trimmedPath = pathname.replace(/\/+$/, '')
    return trimmedPath === '' ? '/' : trimmedPath
}

const decodeRouteSegment = (value: string) => {
    try {
        return decodeURIComponent(value)
    } catch {
        return value
    }
}

export const getRouteState = (pathname: string): { activeTab: AppTab; activeSessionId: string | null } | null => {
    const normalizedPath = normalizePathname(pathname)

    for (const tab of tabOrder) {
        if (normalizedPath === tabRoutes[tab]) {
            return { activeTab: tab, activeSessionId: null }
        }
    }

    if (normalizedPath === '/' || normalizedPath === '/management') {
        return { activeTab: 'overview', activeSessionId: null }
    }

    if (normalizedPath.startsWith(`${tabRoutes.chat}/`)) {
        const sessionId = normalizedPath.slice(`${tabRoutes.chat}/`.length)
        if (sessionId !== '' && !sessionId.includes('/')) {
            return { activeTab: 'chat', activeSessionId: decodeRouteSegment(sessionId) }
        }
        return null
    }

    if (normalizedPath === tabRoutes.contacts) {
        return { activeTab: 'contacts', activeSessionId: null }
    }

    if (normalizedPath === tabRoutes.me) {
        return { activeTab: 'me', activeSessionId: null }
    }

    return null
}

export const buildChatRoute = (sessionId: string, promptId?: string) => {
    const searchParams = new URLSearchParams()
    if (promptId) {
        searchParams.set('promptId', promptId)
    }

    const search = searchParams.toString()
    return `${tabRoutes.chat}/${encodeURIComponent(sessionId)}${search ? `?${search}` : ''}`
}
