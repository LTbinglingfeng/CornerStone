import { useState, useCallback, useRef, useEffect } from 'react'
import { gsap } from 'gsap'
import Header from './components/Header'
import SearchBar from './components/SearchBar'
import ChatList from './components/ChatList'
import ChatDetail from './components/ChatDetail'
import Contacts from './components/Contacts'
import ProfilePage from './components/ProfilePage'
import BottomNav from './components/BottomNav'
import PromptSelector from './components/PromptSelector'
import { createSession } from './services/api'
import './App.css'

function App() {
  const [refreshKey, setRefreshKey] = useState(0)
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [selectedPromptId, setSelectedPromptId] = useState<string | undefined>(undefined)
  const [searchQuery, setSearchQuery] = useState('')
  const [activeTab, setActiveTab] = useState<'chat' | 'contacts' | 'settings'>('chat')
  const [showPromptSelector, setShowPromptSelector] = useState(false)
  const viewsContainerRef = useRef<HTMLDivElement>(null)
  const wrapperRef = useRef<HTMLDivElement>(null)

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
  const getTabIndex = (tab: 'chat' | 'contacts' | 'settings') => {
    switch (tab) {
      case 'chat': return 0
      case 'contacts': return 1
      case 'settings': return 2
    }
  }

  const animateToTab = useCallback((index: number) => {
    if (viewsContainerRef.current && wrapperRef.current) {
      const containerWidth = wrapperRef.current.offsetWidth
      gsap.to(viewsContainerRef.current, {
        x: -index * containerWidth,
        duration: 0.3,
        ease: 'power2.out'
      })
    }
  }, [])

  const handleTabChange = useCallback((tab: 'chat' | 'contacts' | 'settings') => {
    if (tab === activeTab) return
    const newIndex = getTabIndex(tab)
    animateToTab(newIndex)
    setActiveTab(tab)
  }, [activeTab, animateToTab])

  const handleStartChatWithPrompt = useCallback((sessionId: string, promptId: string) => {
    animateToTab(0)
    setActiveTab('chat')
    setSelectedSessionId(sessionId)
    setSelectedPromptId(promptId)
  }, [animateToTab])

  const handleSwitchSession = useCallback((sessionId: string, promptId?: string) => {
    setSelectedSessionId(sessionId)
    setSelectedPromptId(promptId)
  }, [])

  // 初始化位置
  useEffect(() => {
    if (viewsContainerRef.current) {
      gsap.set(viewsContainerRef.current, { x: 0 })
    }
  }, [])

  // 窗口大小变化时重新定位
  useEffect(() => {
    const handleResize = () => {
      const index = getTabIndex(activeTab)
      if (viewsContainerRef.current && wrapperRef.current) {
        const containerWidth = wrapperRef.current.offsetWidth
        gsap.set(viewsContainerRef.current, { x: -index * containerWidth })
      }
    }
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [activeTab])

  return (
    <div className="app-wrapper" ref={wrapperRef}>
      {/* 平行视窗容器 */}
      <div className="views-container" ref={viewsContainerRef}>
        {/* 聊天页面 */}
        <div className="view-page">
          <div className="app-container">
            <div className="main-content">
              <Header title="CornerStone" onAdd={handleCreateSession} />
              <SearchBar value={searchQuery} onChange={setSearchQuery} />
              <ChatList key={refreshKey} onSelectSession={handleSelectSession} searchQuery={searchQuery} />
            </div>
          </div>
        </div>

        {/* 通讯录页面 */}
        <div className="view-page">
          <Contacts
            onStartChat={handleStartChatWithPrompt}
          />
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
      {showPromptSelector && (
        <PromptSelector
          onSelect={handlePromptSelect}
          onClose={handlePromptSelectorClose}
        />
      )}
    </div>
  )
}

export default App
