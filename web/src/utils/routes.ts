export type AppTab = 'chat' | 'contacts' | 'moments' | 'me'

export const tabOrder: AppTab[] = ['chat', 'contacts', 'moments', 'me']

export const tabRoutes: Record<AppTab, string> = {
    chat: '/chat',
    contacts: '/contacts',
    moments: '/moments',
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

    if (normalizedPath === tabRoutes.chat) {
        return { activeTab: 'chat', activeSessionId: null }
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

    if (normalizedPath === tabRoutes.moments) {
        return { activeTab: 'moments', activeSessionId: null }
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
