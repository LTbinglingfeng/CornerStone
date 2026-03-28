import { useState, useCallback, useRef, useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { useLocation, useNavigate } from 'react-router-dom'
import Header from './components/Header'
import SearchBar from './components/SearchBar'
import ChatList from './components/ChatList'
import ChatDetail from './components/ChatDetail'
import Contacts from './components/Contacts'
import MomentsPage from './components/Moments/MomentsPage'
import ProfilePage from './components/ProfilePage'
import BottomNav from './components/BottomNav'
import PromptSelector from './components/PromptSelector'
import AuthSetupPage from './components/AuthSetupPage'
import AuthLoginPage from './components/AuthLoginPage'
import {
    appendQueryParam,
    createSession,
    getAuthStatus,
    getPromptAvatarUrl,
    getSession,
    getSessions,
    loginAuth,
    setAuthToken,
    setupAuth,
} from './services/api'
import { splitAssistantMessageContent } from './components/ChatDetail/utils'
import { formatNotificationBody, getNotificationsEnabled, isNotificationSupported } from './utils/notifications'
import { slideTransition } from './utils/motion'
import { buildChatRoute, getRouteState, normalizePathname, tabOrder, tabRoutes } from './utils/routes'
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
            const session = await createSession(promptName, promptId)
            if (session) {
                openSession(session.id, promptId)
            }
        },
        [openSession]
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
        (tab: 'chat' | 'contacts' | 'moments' | 'me') => {
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
        if (authMode !== 'ready') return

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

                    const record = await getSession(session.id)
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

                    const title = record?.title || record?.prompt_name || '新消息'
                    const icon = record?.prompt_id
                        ? appendQueryParam(getPromptAvatarUrl(record.prompt_id), 't', Date.now())
                        : '/logo_black.jpg'

                    const messageParts = splitAssistantMessageContent(lastMessage.content || '')
                    const bodies =
                        messageParts.length > 0
                            ? messageParts
                                  .map((part) => formatNotificationBody(part) || '收到一条新消息')
                                  .filter((part) => part !== '')
                            : [formatNotificationBody(lastMessage.content || '') || '收到一条新消息']

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
    }, [authMode])

    const handleSetup = useCallback(async (username: string, password: string) => {
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
                return '已存在账号，请直接登录'
            }
            if (status === 400) {
                return '请填写有效的用户名和密码'
            }
            return '创建失败，请稍后重试'
        } finally {
            setAuthLoading(false)
        }
    }, [])

    const handleLogin = useCallback(async (username: string, password: string) => {
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
                return '用户名或密码错误'
            }
            if (status === 409) {
                return '请先设置用户名和密码'
            }
            return '登录失败，请重试'
        } finally {
            setAuthLoading(false)
        }
    }, [])

    if (authMode !== 'ready') {
        return (
            <div className="app-wrapper">
                {authMode === 'loading' && <div className="auth-loading">加载中...</div>}
                {authMode === 'setup' && <AuthSetupPage onSubmit={handleSetup} loading={authLoading} />}
                {authMode === 'login' && (
                    <AuthLoginPage username={authUsername} onSubmit={handleLogin} loading={authLoading} />
                )}
            </div>
        )
    }

    return (
        <div className="app-wrapper">
            {/* 平行视窗容器 */}
            <motion.div
                className="views-container"
                animate={{ x: `${-100 * activeTabIndex}%` }}
                transition={slideTransition}
            >
                {/* 聊天页面 */}
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

                {/* 通讯录页面 */}
                <div className="view-page">
                    <Contacts onStartChat={handleStartChatWithPrompt} />
                </div>

                {/* 朋友圈页面 */}
                <div className="view-page">
                    <MomentsPage />
                </div>

                {/* 我的页面 */}
                <div className="view-page">
                    <ProfilePage />
                </div>
            </motion.div>

            {/* 底部导航栏（固定不动） */}
            <BottomNav activeTab={activeTab} onTabChange={handleTabChange} />

            {/* 聊天详情页面（覆盖层） */}
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

            {/* Prompt 选择器 */}
            <AnimatePresence>
                {showPromptSelector && (
                    <PromptSelector onSelect={handlePromptSelect} onClose={handlePromptSelectorClose} />
                )}
            </AnimatePresence>
        </div>
    )
}

export default App
