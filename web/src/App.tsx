import { useState, useCallback, useRef, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { useLocation, useNavigate } from 'react-router-dom'
import Header from './components/Header'
import SearchBar from './components/SearchBar'
import ChatList from './components/ChatList'
import ChatDetail from './components/ChatDetail'
import Contacts from './components/Contacts'
import ProfilePage from './components/ProfilePage'
import BottomNav from './components/BottomNav'
import PromptSelector from './components/PromptSelector'
import AuthSetupPage from './components/AuthSetupPage'
import AuthLoginPage from './components/AuthLoginPage'
import PersonaEditor from './components/PersonaEditor'
import {
    appendQueryParam,
    createSession,
    getErrorMessage,
    getAuthStatus,
    getConfig,
    getPromptAvatarUrl,
    getSession,
    getSessions,
    loginAuth,
    setAuthToken,
    setupAuth,
} from './services/api'
import { splitAssistantMessageContent } from './components/ChatDetail/utils'
import { useToast } from './contexts/ToastContext'
import { useT } from './contexts/I18nContext'
import { formatNotificationBody, getNotificationsEnabled, isNotificationSupported } from './utils/notifications'
import { DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN, resolveAssistantMessageSplitToken } from './utils/assistantMessageSplit'
import { slideTransition } from './utils/motion'
import { buildChatRoute, getRouteState, normalizePathname, tabOrder, tabRoutes } from './utils/routes'
import { logoBlackDataUrl } from 'virtual:cornerstone-logos'
import './App.css'

const getErrorStatus = (error: unknown): number | undefined => {
    if (typeof error === 'object' && error && 'status' in error) {
        const statusValue = (error as { status?: number }).status
        if (typeof statusValue === 'number') {
            return statusValue
        }
    }
    return undefined
}

function App() {
    const { showToast } = useToast()
    const { t } = useT()
    const location = useLocation()
    const navigate = useNavigate()
    const routeState = getRouteState(location.pathname)
    const activeTab = routeState?.activeTab || 'chat'
    const activeTabIndex = tabOrder.indexOf(activeTab)
    const selectedSessionId = routeState?.activeSessionId || null
    const selectedPromptId = new URLSearchParams(location.search).get('promptId') || undefined
    const [authMode, setAuthMode] = useState<'loading' | 'setup' | 'login' | 'ready'>('loading')
    const [authUsername, setAuthUsername] = useState<string | null>(null)
    const [authLoading, setAuthLoading] = useState(false)
    const [refreshKey, setRefreshKey] = useState(0)
    const [searchQuery, setSearchQuery] = useState('')
    const [showPromptSelector, setShowPromptSelector] = useState(false)
    const [editingPromptId, setEditingPromptId] = useState<string | null>(null)
    const [contactsRefreshToken, setContactsRefreshToken] = useState(0)
    const [assistantMessageSplitToken, setAssistantMessageSplitToken] = useState(DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN)
    const [assistantMessageSplitTokenLoaded, setAssistantMessageSplitTokenLoaded] = useState(false)
    const selectedSessionIdRef = useRef<string | null>(null)
    const openSessionHandlerRef = useRef<(id: string, promptId?: string) => void>(() => {})
    const sessionUpdatedAtRef = useRef<Map<string, string>>(new Map())
    const notificationPollingRef = useRef({ running: false, enabled: false })

    const openSession = useCallback(
        (id: string, promptId?: string) => {
            navigate(buildChatRoute(id, promptId))
        },
        [navigate]
    )

    const handleSelectSession = useCallback(
        (id: string, promptId?: string) => {
            openSession(id, promptId)
        },
        [openSession]
    )

    const handleBack = useCallback(() => {
        navigate(tabRoutes.chat)
        setRefreshKey((k) => k + 1)
    }, [navigate])

    const handleCreateSession = useCallback(async () => {
        setShowPromptSelector(true)
    }, [])

    const handlePromptSelect = useCallback(
        async (promptId: string, promptName: string) => {
            setShowPromptSelector(false)
            try {
                const session = await createSession(promptName, promptId)
                if (session) {
                    openSession(session.id, promptId)
                    return
                }
                showToast(t('chat.createSessionFailed'), 'error')
            } catch (error) {
                showToast(getErrorMessage(error, t('chat.createSessionFailed')), 'error')
            }
        },
        [openSession, showToast, t]
    )

    const handlePromptSelectorClose = useCallback(() => {
        setShowPromptSelector(false)
    }, [])

    const handleOpenSessionFromNotification = useCallback(
        (sessionId: string, promptId?: string) => {
            navigate(buildChatRoute(sessionId, promptId))
        },
        [navigate]
    )

    const handleTabChange = useCallback(
        (tab: 'chat' | 'contacts' | 'me') => {
            const nextPath = tabRoutes[tab]
            if (normalizePathname(location.pathname) === nextPath) return
            navigate(nextPath)
        },
        [location.pathname, navigate]
    )

    const handleStartChatWithPrompt = useCallback(
        (sessionId: string, promptId: string) => {
            openSession(sessionId, promptId)
        },
        [openSession]
    )

    const handleSwitchSession = useCallback(
        (sessionId: string, promptId?: string) => {
            openSession(sessionId, promptId)
        },
        [openSession]
    )

    const handleEditPersona = useCallback((promptId?: string) => {
        setEditingPromptId(promptId ?? '')
    }, [])

    const handlePersonaEditorBack = useCallback(() => {
        setEditingPromptId(null)
        setContactsRefreshToken((k) => k + 1)
    }, [])

    useEffect(() => {
        if (typeof window === 'undefined') return

        const params = new URLSearchParams(location.search)
        const sessionId = params.get('sessionId')
        if (!sessionId) return

        params.delete('sessionId')
        navigate(
            {
                pathname: `${tabRoutes.chat}/${encodeURIComponent(sessionId)}`,
                search: params.toString() ? `?${params.toString()}` : '',
                hash: location.hash,
            },
            { replace: true }
        )
    }, [location.hash, location.search, navigate])

    useEffect(() => {
        if (!('serviceWorker' in navigator)) return

        void navigator.serviceWorker.register('/sw.js').catch(() => {
            // ignore service worker registration errors
        })
    }, [])

    useEffect(() => {
        if (!('serviceWorker' in navigator)) return

        const handleMessage = (event: MessageEvent) => {
            const payload = event.data as unknown
            if (!payload || typeof payload !== 'object') return

            const type = (payload as { type?: unknown }).type
            if (type !== 'OPEN_SESSION') return

            const sessionId = (payload as { sessionId?: unknown }).sessionId
            if (typeof sessionId !== 'string' || sessionId === '') return

            const promptIdValue = (payload as { promptId?: unknown }).promptId
            const promptId = typeof promptIdValue === 'string' && promptIdValue ? promptIdValue : undefined

            handleOpenSessionFromNotification(sessionId, promptId)
        }

        navigator.serviceWorker.addEventListener('message', handleMessage)
        return () => {
            navigator.serviceWorker.removeEventListener('message', handleMessage)
        }
    }, [handleOpenSessionFromNotification])

    useEffect(() => {
        const checkAuth = async () => {
            const status = await getAuthStatus()
            if (!status) {
                setAuthMode('login')
                return
            }
            if (status.needs_setup) {
                setAuthToken(null)
                setAuthUsername(null)
                setAuthMode('setup')
                return
            }
            setAuthUsername(status.username || null)
            if (status.authenticated) {
                setAuthMode('ready')
            } else {
                setAuthToken(null)
                setAuthMode('login')
            }
        }
        checkAuth()
    }, [])

    useEffect(() => {
        if (authMode !== 'ready') {
            setAssistantMessageSplitToken(DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN)
            setAssistantMessageSplitTokenLoaded(false)
            return
        }

        let cancelled = false
        setAssistantMessageSplitToken(DEFAULT_ASSISTANT_MESSAGE_SPLIT_TOKEN)
        setAssistantMessageSplitTokenLoaded(false)
        void (async () => {
            const cfg = await getConfig()
            if (cancelled) return

            if (cfg) {
                setAssistantMessageSplitToken(resolveAssistantMessageSplitToken(cfg.assistant_message_split_token))
            }
            setAssistantMessageSplitTokenLoaded(true)
        })()

        return () => {
            cancelled = true
        }
    }, [authMode])

    useEffect(() => {
        selectedSessionIdRef.current = selectedSessionId
    }, [selectedSessionId])

    useEffect(() => {
        openSessionHandlerRef.current = openSession
    }, [openSession])

    useEffect(() => {
        if (authMode !== 'ready') return
        if (new URLSearchParams(location.search).has('sessionId')) return

        const normalizedPath = normalizePathname(location.pathname)
        if (normalizedPath !== location.pathname) {
            navigate(
                {
                    pathname: normalizedPath,
                    search: location.search,
                    hash: location.hash,
                },
                { replace: true }
            )
            return
        }

        if (normalizedPath === '/' || routeState === null) {
            navigate(tabRoutes.chat, { replace: true })
        }
    }, [authMode, location.hash, location.pathname, location.search, navigate, routeState])

    useEffect(() => {
        if (authMode !== 'ready' || !assistantMessageSplitTokenLoaded) return

        let cancelled = false

        const shouldPoll = () => {
            if (!getNotificationsEnabled()) return false
            if (!isNotificationSupported()) return false
            if (Notification.permission !== 'granted') return false
            return true
        }

        const showChatMessageNotification = (
            sessionId: string,
            title: string,
            body: string,
            options?: { promptId?: string; icon?: string; tag?: string }
        ) => {
            const tag = options?.tag || sessionId

            const showViaServiceWorker = async (): Promise<boolean> => {
                if (!('serviceWorker' in navigator)) return false
                try {
                    const registration =
                        (await navigator.serviceWorker.getRegistration()) ||
                        (await navigator.serviceWorker.register('/sw.js'))

                    if (typeof registration?.showNotification !== 'function') return false

                    await registration.showNotification(title, {
                        body,
                        ...(options?.icon ? { icon: options.icon } : {}),
                        tag,
                        data: { sessionId, promptId: options?.promptId || '' },
                    })
                    return true
                } catch {
                    return false
                }
            }

            void (async () => {
                const usedServiceWorker = await showViaServiceWorker()
                if (usedServiceWorker) return

                try {
                    const notification = new Notification(title, {
                        body,
                        ...(options?.icon ? { icon: options.icon } : {}),
                        tag,
                    })
                    notification.onclick = () => {
                        notification.close()
                        window.focus()
                        openSessionHandlerRef.current(sessionId, options?.promptId)
                    }
                    window.setTimeout(() => notification.close(), 8000)
                } catch {
                    // ignore notification errors
                }
            })()
        }

        const tick = async () => {
            if (cancelled) return
            if (notificationPollingRef.current.running) return

            const enabled = shouldPoll()
            if (notificationPollingRef.current.enabled !== enabled) {
                notificationPollingRef.current.enabled = enabled
                sessionUpdatedAtRef.current = new Map()
            }
            if (!enabled) return

            notificationPollingRef.current.running = true
            try {
                const sessions = await getSessions()
                const currentIds = new Set<string>()
                const inChatDetail = selectedSessionIdRef.current !== null

                for (const session of sessions) {
                    currentIds.add(session.id)
                    const previousUpdatedAt = sessionUpdatedAtRef.current.get(session.id)
                    if (!previousUpdatedAt) {
                        sessionUpdatedAtRef.current.set(session.id, session.updated_at)
                        continue
                    }
                    if (previousUpdatedAt === session.updated_at) continue

                    sessionUpdatedAtRef.current.set(session.id, session.updated_at)
                    if (inChatDetail) continue

                    let record = null
                    try {
                        record = await getSession(session.id)
                    } catch {
                        continue
                    }
                    const messages = record?.messages || []
                    if (messages.length === 0) continue
                    const lastMessage = messages[messages.length - 1]
                    if (lastMessage.role !== 'assistant') continue

                    const previousUpdatedMs = Date.parse(previousUpdatedAt)
                    const lastMessageMs = Date.parse(lastMessage.timestamp)
                    if (
                        Number.isFinite(previousUpdatedMs) &&
                        Number.isFinite(lastMessageMs) &&
                        lastMessageMs <= previousUpdatedMs
                    ) {
                        continue
                    }

                    const title = record?.title || record?.prompt_name || t('chat.newChat')
                    const icon = record?.prompt_id
                        ? appendQueryParam(getPromptAvatarUrl(record.prompt_id), 't', Date.now())
                        : logoBlackDataUrl

                    const messageParts = splitAssistantMessageContent(
                        lastMessage.content || '',
                        assistantMessageSplitToken
                    )
                    const bodies =
                        messageParts.length > 0
                            ? messageParts
                                  .map((part) => formatNotificationBody(part) || t('chat.newMessageReceived'))
                                  .filter((part) => part !== '')
                            : [formatNotificationBody(lastMessage.content || '') || t('chat.newMessageReceived')]

                    bodies.forEach((body, index) => {
                        showChatMessageNotification(session.id, title, body, {
                            promptId: record?.prompt_id,
                            icon,
                            tag: `${session.id}:${lastMessage.timestamp}:${index}`,
                        })
                    })
                }

                for (const id of sessionUpdatedAtRef.current.keys()) {
                    if (!currentIds.has(id)) {
                        sessionUpdatedAtRef.current.delete(id)
                    }
                }
            } catch {
                // ignore polling errors
            } finally {
                notificationPollingRef.current.running = false
            }
        }

        void tick()
        const intervalId = window.setInterval(() => {
            void tick()
        }, 1000)

        return () => {
            cancelled = true
            window.clearInterval(intervalId)
        }
    }, [assistantMessageSplitToken, assistantMessageSplitTokenLoaded, authMode, t])

    const handleSetup = useCallback(
        async (username: string, password: string) => {
            setAuthLoading(true)
            try {
                const session = await setupAuth(username, password)
                setAuthToken(session.token)
                setAuthUsername(session.username)
                setAuthMode('ready')
                return null
            } catch (error) {
                const status = getErrorStatus(error)
                if (status === 409) {
                    return t('auth.accountExists')
                }
                if (status === 400) {
                    return t('auth.invalidCredentials')
                }
                return t('auth.createFailed')
            } finally {
                setAuthLoading(false)
            }
        },
        [t]
    )

    const handleLogin = useCallback(
        async (username: string, password: string) => {
            setAuthLoading(true)
            try {
                const session = await loginAuth(username, password)
                setAuthToken(session.token)
                setAuthUsername(session.username)
                setAuthMode('ready')
                return null
            } catch (error) {
                const status = getErrorStatus(error)
                if (status === 401) {
                    return t('auth.wrongPassword')
                }
                if (status === 409) {
                    return t('auth.setupFirst')
                }
                return t('auth.loginFailed')
            } finally {
                setAuthLoading(false)
            }
        },
        [t]
    )

    if (authMode !== 'ready') {
        return (
            <div className="app-wrapper">
                {authMode === 'loading' && <div className="auth-loading">{t('common.loading')}</div>}
                {authMode === 'setup' && <AuthSetupPage onSubmit={handleSetup} loading={authLoading} />}
                {authMode === 'login' && (
                    <AuthLoginPage username={authUsername} onSubmit={handleLogin} loading={authLoading} />
                )}
            </div>
        )
    }

    return (
        <div className="app-wrapper">
            <motion.div
                className="views-container"
                animate={{ x: `${-100 * activeTabIndex}%` }}
                transition={slideTransition}
            >
                <div className="view-page">
                    <div className="app-container">
                        <div className="main-content">
                            <Header title="CornerStone" onAdd={handleCreateSession} />
                            <SearchBar value={searchQuery} onChange={setSearchQuery} />
                            <ChatList
                                onSelectSession={handleSelectSession}
                                searchQuery={searchQuery}
                                refreshToken={refreshKey}
                            />
                        </div>
                    </div>
                </div>

                <div className="view-page">
                    <Contacts
                        onStartChat={handleStartChatWithPrompt}
                        onEditPersona={handleEditPersona}
                        refreshToken={contactsRefreshToken}
                    />
                </div>

                <div className="view-page">
                    <ProfilePage
                        assistantMessageSplitToken={assistantMessageSplitToken}
                        onAssistantMessageSplitTokenChange={setAssistantMessageSplitToken}
                    />
                </div>
            </motion.div>

            <BottomNav activeTab={activeTab} onTabChange={handleTabChange} />

            <AnimatePresence>
                {selectedSessionId && (
                    <ChatDetail
                        key="chat-detail"
                        sessionId={selectedSessionId}
                        promptId={selectedPromptId}
                        onBack={handleBack}
                        onSwitchSession={handleSwitchSession}
                    />
                )}
            </AnimatePresence>

            <AnimatePresence>
                {showPromptSelector && (
                    <PromptSelector onSelect={handlePromptSelect} onClose={handlePromptSelectorClose} />
                )}
            </AnimatePresence>

            <AnimatePresence>
                {editingPromptId !== null && (
                    <PersonaEditor
                        key="persona-editor"
                        promptId={editingPromptId || undefined}
                        onBack={handlePersonaEditorBack}
                    />
                )}
            </AnimatePresence>
        </div>
    )
}

export default App
