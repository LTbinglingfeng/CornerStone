import { useState, useCallback, useRef, useEffect, useLayoutEffect } from 'react'
import { gsap } from 'gsap'
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
import { createSession, getAuthStatus, setupAuth, loginAuth, setAuthToken, getSessions, getSession } from './services/api'
import { formatNotificationBody, getNotificationsEnabled, isNotificationSupported } from './utils/notifications'
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
    const [authMode, setAuthMode] = useState<'loading' | 'setup' | 'login' | 'ready'>('loading')
    const [authUsername, setAuthUsername] = useState<string | null>(null)
    const [authLoading, setAuthLoading] = useState(false)
    const [refreshKey, setRefreshKey] = useState(0)
    const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
    const [selectedPromptId, setSelectedPromptId] = useState<string | undefined>(undefined)
    const [searchQuery, setSearchQuery] = useState('')
    const [activeTab, setActiveTab] = useState<'chat' | 'contacts' | 'moments' | 'settings'>('chat')
    const [showPromptSelector, setShowPromptSelector] = useState(false)
    const viewsContainerRef = useRef<HTMLDivElement>(null)
    const selectedSessionIdRef = useRef<string | null>(null)
    const selectSessionHandlerRef = useRef<(id: string, promptId?: string) => void>(() => {})
    const sessionUpdatedAtRef = useRef<Map<string, string>>(new Map())
    const notificationPollingRef = useRef({ running: false, enabled: false })

    const handleSelectSession = useCallback((id: string, promptId?: string) => {
        setSelectedSessionId(id)
        setSelectedPromptId(promptId)
    }, [])

    const handleBack = useCallback(() => {
        setSelectedSessionId(null)
        setSelectedPromptId(undefined)
        setRefreshKey((k) => k + 1)
    }, [])

    const handleCreateSession = useCallback(async () => {
        setShowPromptSelector(true)
    }, [])

    const handlePromptSelect = useCallback(async (promptId: string, promptName: string) => {
        setShowPromptSelector(false)
        const session = await createSession(promptName, promptId)
        if (session) {
            setSelectedSessionId(session.id)
            setSelectedPromptId(promptId)
        }
    }, [])

    const handlePromptSelectorClose = useCallback(() => {
        setShowPromptSelector(false)
    }, [])

    // 获取tab对应的索引
    const getTabIndex = (tab: 'chat' | 'contacts' | 'moments' | 'settings') => {
        switch (tab) {
            case 'chat':
                return 0
            case 'contacts':
                return 1
            case 'moments':
                return 2
            case 'settings':
                return 3
        }
    }

    const setTabPosition = useCallback((index: number) => {
        if (!viewsContainerRef.current) return
        gsap.set(viewsContainerRef.current, { xPercent: -100 * index, force3D: true })
    }, [])

    const animateToTab = useCallback((index: number) => {
        if (!viewsContainerRef.current) return
        gsap.to(viewsContainerRef.current, {
            xPercent: -100 * index,
            duration: 0.3,
            ease: 'power2.out',
            force3D: true,
            overwrite: 'auto',
        })
    }, [])

    const handleTabChange = useCallback(
        (tab: 'chat' | 'contacts' | 'moments' | 'settings') => {
            if (tab === activeTab) return
            const newIndex = getTabIndex(tab)
            animateToTab(newIndex)
            setActiveTab(tab)
        },
        [activeTab, animateToTab]
    )

    const handleStartChatWithPrompt = useCallback(
        (sessionId: string, promptId: string) => {
            animateToTab(0)
            setActiveTab('chat')
            setSelectedSessionId(sessionId)
            setSelectedPromptId(promptId)
        },
        [animateToTab]
    )

    const handleSwitchSession = useCallback((sessionId: string, promptId?: string) => {
        setSelectedSessionId(sessionId)
        setSelectedPromptId(promptId)
    }, [])

    // 初始化位置：在首帧绘制前设置，避免闪屏
    useLayoutEffect(() => {
        setTabPosition(getTabIndex(activeTab))
    }, [setTabPosition])

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
        selectSessionHandlerRef.current = handleSelectSession
    }, [handleSelectSession])

    useEffect(() => {
        if (authMode !== 'ready') return

        let cancelled = false

        const shouldPoll = () => {
            if (!getNotificationsEnabled()) return false
            if (!isNotificationSupported()) return false
            if (Notification.permission !== 'granted') return false
            return true
        }

        const showChatMessageNotification = (sessionId: string, title: string, body: string, promptId?: string) => {
            const notification = new Notification(title, {
                body,
                icon: '/logo_black.jpg',
                tag: sessionId,
            })
            notification.onclick = () => {
                notification.close()
                window.focus()
                selectSessionHandlerRef.current(sessionId, promptId)
            }
            window.setTimeout(() => notification.close(), 8000)
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
                    if (Number.isFinite(previousUpdatedMs) && Number.isFinite(lastMessageMs) && lastMessageMs <= previousUpdatedMs) {
                        continue
                    }

                    const title = record?.title || record?.prompt_name || '新消息'
                    const body = formatNotificationBody(lastMessage.content || '') || '收到一条新消息'
                    showChatMessageNotification(session.id, title, body, record?.prompt_id)
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
            <div className="views-container" ref={viewsContainerRef}>
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
            </div>

            {/* 底部导航栏（固定不动） */}
            <BottomNav activeTab={activeTab} onTabChange={handleTabChange} />

            {/* 聊天详情页面（覆盖层） */}
            {selectedSessionId && (
                <ChatDetail
                    sessionId={selectedSessionId}
                    promptId={selectedPromptId}
                    onBack={handleBack}
                    onSwitchSession={handleSwitchSession}
                />
            )}

            {/* Prompt 选择器 */}
            {showPromptSelector && <PromptSelector onSelect={handlePromptSelect} onClose={handlePromptSelectorClose} />}
        </div>
    )
}

export default App
